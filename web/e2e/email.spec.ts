import { expect, test, type APIRequestContext } from "@playwright/test";
import { api, projectKey, run, signIn } from "./helpers";

// Email notifications: preferences in the profile, an immediate email (read from the
// development mailbox, /api/v1/dev/mailbox), one-click unsubscribe from its link, and the
// admin test email. Immediate emails wait WFM_EMAIL_BATCH_WINDOW (2 minutes by default; set it
// to a few seconds for faster runs), so the email check allows for the full window.

type Sent = { to: string[]; subject: string; text: string; html: string; headers?: Record<string, string> };

async function mailbox(request: APIRequestContext, to: string): Promise<Sent[]> {
  const res = await request.get(`/api/v1/dev/mailbox?to=${encodeURIComponent(to)}`);
  expect(res.ok(), "the development mailbox needs WFM_ENV=development and no WFM_SMTP_HOST").toBeTruthy();
  return res.json();
}

test("email notifications: preferences, a combined email, one-click unsubscribe and the admin test email", async ({ page, browser }) => {
  test.setTimeout(240_000);
  // The admin signs in first so a fresh database makes them an organisation admin.
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const admin = api(page.request);

  // A new person per attempt: the test changes their settings.
  const melEmail = `e2e-mail-${run.toLowerCase()}-${Date.now().toString(36)}@example.com`;
  const ctx = await browser.newContext();
  const mel = await ctx.newPage();
  await signIn(mel, melEmail, "Mel Mail");
  const melUser = await api(mel.request).get("/me");

  // Profile → Email notifications: defaults, then comments immediately and a digest at 18:00.
  await mel.goto("/profile#email-notifications");
  const section = mel.locator("#email-notifications");
  await expect(section.getByRole("heading", { name: "Email notifications" })).toBeVisible();
  await expect(section.getByText(`Sent to ${melEmail}`)).toBeVisible();
  await expect(section.getByRole("radio", { name: "Assigned to me: Immediately" })).toBeChecked();
  await expect(section.getByRole("radio", { name: "Comments: Daily digest" })).toBeChecked();
  await section.getByRole("radio", { name: "Comments: Immediately" }).check();
  await expect(section.getByRole("status")).toHaveText("Saved");
  await section.getByLabel("Daily digest at").selectOption("18");
  await expect(section.getByRole("status")).toHaveText("Saved");
  await mel.reload();
  await expect(section.getByRole("radio", { name: "Comments: Immediately" })).toBeChecked();
  await expect(section.getByLabel("Daily digest at")).toHaveValue("18");

  // Ada assigns Mel an issue and comments on it: one email, once the batch window has passed.
  const key = projectKey("EM");
  await admin.post("/projects", { key, name: `Email ${key}`, template: "kanban" });
  await admin.post("/issues", { projectKey: key, typeKey: "task", summary: "Renew the TLS certificate", assigneeId: melUser.id });
  await admin.post(`/issues/${key}-1/comments`, { body: "The old one expires on **Friday**." });
  await expect
    .poll(async () => (await mailbox(page.request, melEmail)).length, { timeout: 200_000, intervals: [2_000] })
    .toBeGreaterThan(0);
  const [mail] = await mailbox(page.request, melEmail);
  expect(mail.subject).toBe(`[${key}-1] Renew the TLS certificate`);
  expect(mail.text).toContain(`2 updates on ${key}-1`);
  expect(mail.text).toContain("The old one expires on **Friday**.");
  expect(mail.html).toContain(`/browse/${key}-1`);
  expect(mail.headers?.["List-Unsubscribe-Post"]).toBe("List-Unsubscribe=One-Click");
  // Nobody is emailed about their own changes.
  expect((await mailbox(page.request, "e2e-admin@example.com")).filter((m) => m.subject.includes(`${key}-1`))).toHaveLength(0);

  // The email's unsubscribe link asks first, then switches those emails off.
  const unsubscribe = new URL(mail.headers!["List-Unsubscribe"].replace(/^<|>$/g, ""));
  await mel.goto(unsubscribe.pathname + unsubscribe.search);
  await expect(mel.getByRole("heading", { name: "Unsubscribe from emails about assignments and comments?" })).toBeVisible();
  await mel.getByRole("button", { name: "Unsubscribe", exact: true }).click();
  await expect(mel.getByRole("heading", { name: "You're unsubscribed" })).toBeVisible();
  await mel.getByRole("link", { name: "Manage email notifications" }).click();
  await expect(section.getByRole("radio", { name: "Assigned to me: Off" })).toBeChecked();
  await expect(section.getByRole("radio", { name: "Comments: Off" })).toBeChecked();
  await expect(section.getByRole("radio", { name: "Status changes: Daily digest" })).toBeChecked();
  await ctx.close();

  // Administration → Email: configuration and a test email to yourself.
  await page.goto("/admin/email");
  await expect(page.getByRole("heading", { name: "Outgoing email" })).toBeVisible();
  await expect(page.getByText("In memory only")).toBeVisible();
  const before = (await mailbox(page.request, "e2e-admin@example.com")).length;
  await page.getByRole("button", { name: "Send test email" }).click();
  await expect(page.getByText("Test email kept in memory for e2e-admin@example.com")).toBeVisible();
  const adminMail = await mailbox(page.request, "e2e-admin@example.com");
  expect(adminMail).toHaveLength(before + 1);
  expect(adminMail[adminMail.length - 1].subject).toBe("Test email from Workflow");
});
