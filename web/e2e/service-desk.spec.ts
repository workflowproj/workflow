import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import { api, projectKey, run, signIn } from "./helpers";

// The service desk end to end: an agent creates a service project and invites a customer; the
// customer chooses a password, signs in to the help centre and raises a request through a form;
// the agent replies to the customer and adds an internal note, and the customer sees only the
// reply. Then an approval: a request type asks a manager, who approves it in the help centre.
// SLAs show in the agent's queue and issue view. Needs the development mailbox (no SMTP).

async function mailLink(request: APIRequestContext, email: string, subject: string, pattern: RegExp): Promise<string> {
  let link = "";
  await expect
    .poll(async () => {
      const msgs: { subject: string; text: string }[] = await (await request.get(`/api/v1/dev/mailbox?to=${encodeURIComponent(email)}`)).json();
      const m = msgs.filter((x) => x.subject.includes(subject)).pop();
      link = m?.text.match(pattern)?.[0] ?? "";
      return link;
    })
    .not.toBe("");
  const u = new URL(link);
  return u.pathname + u.search;
}

async function mailSubjects(request: APIRequestContext, email: string): Promise<string[]> {
  const msgs: { subject: string }[] = await (await request.get(`/api/v1/dev/mailbox?to=${encodeURIComponent(email)}`)).json();
  return msgs.map((m) => m.subject);
}

async function mailTexts(request: APIRequestContext, email: string): Promise<string> {
  const msgs: { text: string }[] = await (await request.get(`/api/v1/dev/mailbox?to=${encodeURIComponent(email)}`)).json();
  return msgs.map((m) => m.text).join("\n---\n");
}

/** requestKey reads the request's key from the portal request page's address. */
function requestKey(page: Page): string {
  return decodeURIComponent(new URL(page.url()).pathname.split("/").pop() ?? "");
}

async function passwordSignIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByRole("tab", { name: "Password" }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("service desk: customer request, public reply and internal note, approval and SLAs", async ({ page, browser }) => {
  test.setTimeout(150_000);
  const key = projectKey("SD");
  const deskName = `Help ${key}`;
  const customerEmail = `e2e-customer-${run.toLowerCase()}@example.com`;
  const customerName = `Cora Customer ${run}`;
  const password = "Copper-Meadow-Lantern-73";
  const approverEmail = `e2e-approver-${run.toLowerCase()}@example.com`;
  const approverName = `Arlo Approver ${run}`;

  // The agent signs in first (the first account is the organisation admin); the approver has an
  // organisation account too (dev sign-in creates it).
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const approverCtx = await browser.newContext();
  const approver = await approverCtx.newPage();
  await signIn(approver, approverEmail, approverName);

  // The agent creates a service project; it is private by default and opens on its queues.
  await page.goto("/projects");
  await page.getByRole("button", { name: "New project" }).first().click();
  const create = page.getByRole("dialog", { name: "New project" });
  await create.getByLabel("Name").fill(deskName);
  await create.getByLabel("Key").fill(key);
  await create.getByRole("button", { name: /Service desk/ }).click();
  await expect(create.getByLabel("Access")).toHaveValue("private");
  await create.getByRole("button", { name: "Create project" }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${key}/queues`));
  await expect(page.getByRole("navigation", { name: "Queues" }).getByRole("button", { name: /^Unassigned/ })).toBeVisible();

  // The agent invites a customer from the Customers page.
  await page.getByRole("link", { name: "Customers" }).click();
  await page.getByRole("button", { name: "Add customer" }).first().click();
  const add = page.getByRole("dialog", { name: "Add customer" });
  await add.getByLabel("Email").fill(customerEmail);
  await add.getByLabel("Name").fill(customerName);
  await add.getByRole("button", { name: "Add customer" }).click();
  // The development server has no SMTP server: the agent is told the invitation was not emailed.
  await expect(page.getByText(`Added ${customerName}, but their invitation was not emailed`, { exact: false })).toBeVisible();
  await expect(page.getByRole("row", { name: new RegExp(customerName) }).getByText("Invited")).toBeVisible();
  // The project list opens the desk on its queues, like the sidebar.
  await page.goto("/projects");
  await expect(page.getByRole("table").getByRole("link", { name: deskName })).toHaveAttribute("href", `/projects/${key}/queues`);

  // The customer follows the invitation, chooses a password and signs in to the help centre.
  const invite = await mailLink(page.request, customerEmail, "account", /https?:\/\/\S+\/invite\?token=\S+/);
  const customerCtx = await browser.newContext();
  const customer = await customerCtx.newPage();
  await customer.goto(invite);
  await customer.getByLabel("Password", { exact: true }).fill(password);
  await customer.getByRole("button", { name: "Set password" }).click();
  await expect(customer.getByText("Your password is set")).toBeVisible();
  await passwordSignIn(customer, customerEmail, password);
  await expect(customer).toHaveURL(/\/portal$/);
  // The agent app is not for customers.
  await customer.goto(`/projects/${key}/queues`);
  await expect(customer).toHaveURL(/\/portal$/);

  // The customer raises a request through the "Get IT help" form.
  await customer.getByRole("link", { name: new RegExp(deskName) }).click();
  await expect(customer.getByRole("heading", { name: "What can we help you with?" })).toBeVisible();
  await customer.getByRole("link", { name: /Get IT help/ }).click();
  await customer.getByLabel(/Summary/).fill("My laptop will not start");
  await customer.getByLabel(/Details/).fill("It beeps three times and stays dark.");
  await customer.getByRole("button", { name: "Send request" }).click();
  await expect(customer.getByText(/Your request .* was sent/)).toBeVisible();
  const issueKey = requestKey(customer);
  expect(issueKey).toMatch(new RegExp(`^${key}-\\d+$`));
  await expect.poll(() => mailSubjects(page.request, customerEmail)).toContainEqual(expect.stringContaining(issueKey));

  // The agent sees the request in the queue, with its SLAs.
  await page.goto(`/projects/${key}/queues`);
  const row = page.getByRole("row", { name: new RegExp(issueKey) });
  await expect(row).toBeVisible();
  await expect(row).toContainText(/Time to first response: [0-9hm ]+ left/);
  await expect(row).toContainText(/Time to resolution: [0-9hm ]+ left/);

  // The agent replies to the customer and adds an internal note.
  await page.goto(`/browse/${issueKey}`);
  const panel = page.getByRole("region", { name: "Service request" });
  await expect(panel.getByText("Time to first response", { exact: true })).toBeVisible();
  await expect(panel.getByText("Time to resolution", { exact: true })).toBeVisible();
  await expect(panel.getByLabel("Request type")).toHaveValue(/.+/);
  // No repository is connected to the desk: no Development section asking for one.
  await expect(page.getByRole("heading", { name: "Development" })).toHaveCount(0);
  await page.getByRole("button", { name: "Reply to customer" }).click();
  await page.locator(".tiptap").last().click();
  await page.keyboard.type("Please hold the power button for ten seconds.");
  await page.getByRole("button", { name: "Send reply" }).click();
  await expect(page.getByText("Please hold the power button for ten seconds.")).toBeVisible();
  await page.getByRole("button", { name: "Add internal note" }).click();
  await page.locator(".tiptap").last().click();
  await page.keyboard.type("Probably the RAM; order a spare.");
  await expect(page.getByRole("radio", { name: "Internal note" })).toHaveAttribute("aria-checked", "true");
  await page.getByRole("button", { name: "Add internal note" }).click();
  await expect(page.getByText("Probably the RAM; order a spare.")).toBeVisible();
  await expect(page.locator("li", { hasText: "Probably the RAM; order a spare." }).getByText("Internal note")).toBeVisible();
  await expect(page.locator("li", { hasText: "Please hold the power button" }).getByText("Reply to customer")).toBeVisible();
  // The first reply to the customer met the first-response goal.
  await expect(panel.locator("li", { hasText: "Time to first response" })).toContainText(/Met/i);

  // The customer sees the reply, never the note (in the portal and in emails).
  await customer.reload();
  await expect(customer.getByText("Please hold the power button for ten seconds.")).toBeVisible();
  await expect(customer.getByText("Probably the RAM")).toHaveCount(0);
  await expect.poll(() => mailTexts(page.request, customerEmail)).toContain("Please hold the power button");
  expect(await mailTexts(page.request, customerEmail)).not.toContain("Probably the RAM");
  // The customer answers from the portal.
  await customer.getByLabel("Reply to the team").fill("That worked, thank you!");
  await customer.getByRole("button", { name: "Send reply" }).click();
  await expect(customer.getByText("That worked, thank you!")).toBeVisible();

  // Approvals: "Request new hardware" now asks the approver first.
  const a = api(page.request);
  const users: { id: string; email: string }[] = await a.get(`/users?q=${encodeURIComponent(approverEmail)}`);
  const approverId = users.find((u) => u.email === approverEmail)!.id;
  const types: {
    id: string;
    name: string;
    description: string;
    icon: string;
    issueType: { key: string };
    group: string;
    position: number;
    fields: { field: string; label: string; required: boolean }[];
  }[] = await a.get(`/projects/${key}/request-types`);
  const hardware = types.find((t) => t.name === "Request new hardware")!;
  const res = await page.request.put(`/api/v1/request-types/${hardware.id}`, {
    headers: { "X-Requested-With": "e2e" },
    data: {
      name: hardware.name,
      description: hardware.description,
      icon: hardware.icon,
      issueTypeKey: hardware.issueType.key,
      group: hardware.group,
      position: hardware.position,
      fields: hardware.fields.map((f) => ({ field: f.field, label: f.label, required: f.required })),
      approval: { mode: "any", approverIds: [approverId], approvedStatus: "Waiting for support", declinedStatus: "Declined" },
    },
  });
  expect(res.ok(), await res.text()).toBeTruthy();

  await customer.goto(`/portal/${key}`);
  await customer.getByRole("link", { name: /Request new hardware/ }).click();
  await expect(customer.getByText(/needs approval/i)).toBeVisible();
  await customer.getByLabel(/What do you need/).fill("A second monitor");
  await customer.getByRole("button", { name: "Send request" }).click();
  await expect(customer.getByText(/Your request .* was sent/)).toBeVisible();
  const hardwareKey = requestKey(customer);
  await expect(customer.getByText("Waiting for approval")).toBeVisible();
  await expect.poll(() => mailSubjects(page.request, approverEmail)).toContainEqual(expect.stringContaining(hardwareKey));

  // The approver approves in the help centre, with a comment.
  await approver.goto("/portal/approvals");
  await approver.getByRole("link", { name: new RegExp(hardwareKey) }).click();
  await approver.getByLabel("Comment (optional)").fill("Within budget.");
  await approver.getByRole("button", { name: "Approve" }).click();
  await expect(approver.getByText("has not decided yet")).toHaveCount(0);
  await expect(approver.getByText("“Within budget.”")).toBeVisible();

  // The request moved on; the customer and the agent see the decision.
  await customer.reload();
  await expect(customer.getByText(/approved/).first()).toBeVisible();
  await expect(customer.getByText("Waiting for support").first()).toBeVisible();
  await page.goto(`/browse/${hardwareKey}`);
  await expect(page.getByRole("region", { name: "Service request" }).getByText("approved", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "History" }).click();
  await expect(page.getByText(`${approverName} approved: Within budget.`)).toBeVisible();

  await customerCtx.close();
  await approverCtx.close();
});
