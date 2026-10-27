import { createHmac } from "node:crypto";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import { run, signIn } from "./helpers";

// Local accounts: an admin invites someone by email; they follow the link from the email,
// choose a password, sign in, set up two-factor authentication (the test computes the TOTP
// codes) and sign in again with a code. Needs the development mailbox (no SMTP configured).

function base32(s: string): Buffer {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = 0;
  let value = 0;
  const out: number[] = [];
  for (const c of s.replace(/[\s=]/g, "").toUpperCase()) {
    value = (value << 5) | alphabet.indexOf(c);
    bits += 5;
    if (bits >= 8) {
      out.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(out);
}

/** totp computes the RFC 6238 code `steps` 30-second steps from now. */
function totp(secret: string, steps = 0): string {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30_000) + steps));
  const h = createHmac("sha1", base32(secret)).update(counter).digest();
  const o = h[h.length - 1] & 0xf;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).padStart(6, "0");
}

async function inviteLink(request: APIRequestContext, email: string): Promise<string> {
  let link = "";
  await expect
    .poll(async () => {
      const msgs: { subject: string; text: string }[] = await (await request.get(`/api/v1/dev/mailbox?to=${encodeURIComponent(email)}`)).json();
      const m = msgs.filter((x) => x.subject.includes("invited")).pop();
      link = m?.text.match(/https?:\/\/\S+\/invite\?token=\S+/)?.[0] ?? "";
      return link;
    })
    .not.toBe("");
  const u = new URL(link);
  return u.pathname + u.search;
}

async function passwordSignIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByRole("tab", { name: "Password" }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("invite a local account, set a password, set up two-factor and sign in with a code", async ({ page, browser }) => {
  test.setTimeout(90_000);
  const email = `e2e-local-${run.toLowerCase()}@example.com`;
  const name = `Lola Local ${run}`;
  const password = "Velvet-Harbour-Lantern-58";

  // The admin adds a person who will sign in with a password, by invitation.
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  await page.goto("/admin/users");
  await page.getByRole("button", { name: "Add person" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a person" });
  await dialog.getByLabel("Display name").fill(name);
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByRole("radio", { name: /Email invitation/ }).click();
  await dialog.getByRole("button", { name: "Create" }).click();
  // Without an SMTP server the invitation exists but is not emailed, and the admin is told so.
  const notEmailed = page.getByRole("dialog", { name: "Invitation not emailed" });
  await expect(notEmailed).toContainText(`it did not reach ${email}`);
  await expect(notEmailed.getByRole("button", { name: "Set temporary password" })).toBeVisible();
  await notEmailed.getByRole("button", { name: "Done" }).click();
  const row = page.getByRole("row", { name: new RegExp(name) });
  await expect(row.getByText("Invited")).toBeVisible();

  // The invitee follows the link from the email in their own browser.
  const path = await inviteLink(page.request, email);
  const ctx = await browser.newContext();
  const invitee = await ctx.newPage();
  await invitee.goto(path);
  await expect(invitee.getByRole("heading", { name: `Welcome, ${name}` })).toBeVisible();
  const pw = invitee.getByLabel("Password", { exact: true });
  await pw.fill("password1234");
  await expect(invitee.getByText(/most common passwords/)).toBeVisible();
  await pw.fill(password);
  await expect(invitee.getByText("Strong", { exact: true })).toBeVisible();
  await invitee.getByRole("button", { name: "Set password" }).click();
  await expect(invitee.getByText("Your password is set")).toBeVisible();

  // Sign in with the new password.
  await invitee.getByRole("tab", { name: "Password" }).click();
  await expect(invitee.getByLabel("Email")).toHaveValue(email);
  await invitee.getByLabel("Password", { exact: true }).fill("Not-The-Password-123");
  await invitee.getByRole("button", { name: "Sign in" }).click();
  await expect(invitee.getByText("Incorrect email or password.")).toBeVisible();
  await invitee.getByLabel("Password", { exact: true }).fill(password);
  await invitee.getByRole("button", { name: "Sign in" }).click();
  await expect(invitee).toHaveURL(/\/projects/);

  // Set up two-factor authentication from the profile.
  await invitee.goto("/profile");
  const security = invitee.getByRole("region", { name: "Sign-in and security" }).or(invitee.locator("section", { hasText: "Sign-in and security" }));
  await security.getByRole("button", { name: "Set up" }).click();
  const setup = invitee.getByRole("dialog", { name: "Set up two-factor authentication" });
  await expect(setup.getByRole("img", { name: /QR code/ })).toBeVisible();
  const secret = (await setup.getByTestId("totp-secret").getAttribute("data-secret")) ?? "";
  expect(secret).toMatch(/^[A-Z2-7]{32}$/);
  await setup.getByLabel(/6-digit code/).fill(totp(secret));
  await setup.getByRole("button", { name: "Turn on" }).click();
  const done = invitee.getByRole("dialog", { name: "Two-factor authentication is on" });
  await expect(done.getByTestId("recovery-codes").locator("li")).toHaveCount(10);
  const codes = (await done.getByTestId("recovery-codes").locator("li").allInnerTexts()).map((c) => c.trim());
  await done.getByLabel("I have saved my recovery codes").check();
  await done.getByRole("button", { name: "Done" }).click();
  await expect(security.getByText("On", { exact: true })).toBeVisible();
  await expect(security.getByText("10 of 10 recovery codes left", { exact: false })).toBeVisible();

  // Sign out, then back in: the password alone is not enough.
  await invitee.getByRole("button", { name: "Account menu" }).click();
  await invitee.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(invitee).toHaveURL(/\/login/);
  await passwordSignIn(invitee, email, password);
  await expect(invitee.getByRole("heading", { name: "Two-factor authentication" })).toBeVisible();
  await invitee.getByLabel("Authentication code").fill("000000");
  await invitee.getByRole("button", { name: "Verify" }).click();
  await expect(invitee.getByText(/code is not valid/)).toBeVisible();
  // The next step's code: the set-up already used the current one.
  await invitee.getByLabel("Authentication code").fill(totp(secret, 1));
  await invitee.getByRole("button", { name: "Verify" }).click();
  await expect(invitee).toHaveURL(/\/projects/);

  // A recovery code works too, once.
  await invitee.getByRole("button", { name: "Account menu" }).click();
  await invitee.getByRole("menuitem", { name: "Sign out" }).click();
  await passwordSignIn(invitee, email, password);
  await invitee.getByRole("button", { name: /Use a recovery code/ }).click();
  await invitee.getByLabel("Recovery code").fill(codes[0]);
  await invitee.getByRole("button", { name: "Verify" }).click();
  await expect(invitee).toHaveURL(/\/projects/);
  await ctx.close();

  // The admin sees a local account with two-factor on.
  await page.reload();
  await expect(row.getByText("Local account")).toBeVisible();
  await expect(row.getByText("2FA")).toBeVisible();
});
