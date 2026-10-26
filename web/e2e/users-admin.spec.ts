import { expect, test, type Locator } from "@playwright/test";
import { api, projectKey, run, signIn } from "./helpers";

// People in administration and the controls around them, at a laptop's 1280 px: a help desk
// customer's organisation role reads "Customer" and changing it must be confirmed; selects are
// wide enough for their options; adding project members never offers customers; and the
// command shown with a new token wraps instead of scrolling out of sight.

test.use({ viewport: { width: 1280, height: 900 } });

/** fitsText: the select's chosen option fits beside the browser's arrow (about 24 px). */
async function fitsText(select: Locator) {
  return select.evaluate((el) => {
    const doc = el.ownerDocument;
    const s = doc.defaultView.getComputedStyle(el);
    const ctx = doc.createElement("canvas").getContext("2d");
    ctx.font = `${s.fontWeight} ${s.fontSize} ${s.fontFamily}`;
    const text: number = ctx.measureText(el.selectedOptions[0]?.text ?? "").width;
    return text + parseFloat(s.paddingLeft) + parseFloat(s.paddingRight) + 24 <= el.clientWidth;
  });
}

test("users: customers read as customers, and selects show their whole text", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const key = projectKey("UA");
  await a.post("/projects", { key, name: `People ${key}`, template: "service" });
  const name = `Cody Customer ${run}`;
  const email = `e2e-cody-${run.toLowerCase()}@example.com`;
  await a.post(`/projects/${key}/customers`, { email, displayName: name });
  const member = await a.post("/admin/users", { displayName: `Mia Member ${run}`, email: `e2e-mia-${run.toLowerCase()}@example.com` });

  await page.goto("/admin/users");
  await page.getByLabel("Search users").fill(run.toLowerCase());
  const role = page.getByLabel(`Organisation role of ${name}`);
  await expect(role).toHaveValue("customer");
  await expect(role.locator("option:checked")).toHaveText("Customer");
  expect(await fitsText(role)).toBe(true);
  const memberRole = page.getByLabel(`Organisation role of ${member.displayName}`);
  await expect(memberRole.locator("option:checked")).toHaveText("Member");
  expect(await fitsText(memberRole)).toBe(true);

  // Turning a customer into an agent is deliberate: cancelling leaves them a customer.
  await role.selectOption("member");
  const confirm = page.getByRole("dialog", { name: `Make ${name} a member?` });
  await expect(confirm).toContainText("help desk customer");
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(role).toHaveValue("customer");
  const listed: { orgRole: string }[] = await a.get(`/admin/users?q=${encodeURIComponent(email)}`);
  expect(listed[0].orgRole).toBe("customer");

  // New project → Access.
  await page.goto("/projects");
  await page.getByRole("button", { name: "New project" }).first().click();
  const access = page.getByRole("dialog", { name: "New project" }).getByLabel("Access");
  await expect(access.locator("option:checked")).toHaveText("Open to the organisation");
  expect(await fitsText(access)).toBe(true);

  // Add members: role names in the select, the chosen role's description under it, and no
  // customers among the people offered.
  await page.goto(`/projects/${key}/settings/members`);
  await page.getByRole("button", { name: "Add members" }).first().click();
  const add = page.getByRole("dialog", { name: "Add members" });
  const roleSelect = add.getByLabel("Role");
  await expect(roleSelect.locator("option:checked")).toHaveText("Member");
  expect(await fitsText(roleSelect)).toBe(true);
  await expect(add.getByText("Create, edit, transition and comment on issues; run sprints.")).toBeVisible();
  await add.getByRole("button", { name: "Person" }).click();
  await page.getByPlaceholder("Search people…").fill(run.toLowerCase());
  await expect(page.getByRole("button", { name: new RegExp(member.displayName) })).toBeVisible();
  await expect(page.getByRole("button", { name: new RegExp(name) })).toHaveCount(0);
});

test("a new token's commands wrap instead of scrolling", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  await page.goto("/profile");
  await page.getByRole("button", { name: "New token" }).first().click();
  const dialog = page.getByRole("dialog", { name: "New token" });
  await dialog.getByLabel("Name").fill(`Assistant ${run}`);
  await dialog.getByRole("button", { name: "Create token" }).click();
  const created = page.getByRole("dialog", { name: /created/ });
  await created.getByRole("tab", { name: "Claude Code" }).click();
  const command = created.getByRole("tabpanel").locator("pre");
  await expect(command).toContainText('--header "Authorization: Bearer wfm_pat_');
  expect(await command.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
});
