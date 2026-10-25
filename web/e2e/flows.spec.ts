import { expect, test, type Page } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Core flows through the UI. Each test creates its own project through the API, so the suite
// can run repeatedly against the same deployment.

async function setup(page: Page, prefix: string, template: string) {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey(prefix);
  const a = api(page.request);
  await a.post("/projects", { key, name: `${prefix} ${key}`, template });
  return { key, a };
}

test("issue view: edit, comment and a transition blocked by a validator", async ({ page }) => {
  const { key, a } = await setup(page, "IV", "scrum");
  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Original summary" });
  await a.post("/issues", { projectKey: key, typeKey: "subtask", summary: "Open sub-task", parentKey: story.key });
  await a.post(`/issues/${story.key}/transitions`, { transitionId: "start-work" });

  await page.goto(`/browse/${story.key}`);
  const heading = page.getByRole("heading", { level: 1, name: "Original summary" });
  await expect(heading).toBeVisible();

  // Inline summary edit: the heading turns into a text box with its text selected.
  await heading.getByRole("button").click();
  await page.getByLabel("Summary").fill("Edited summary");
  await page.getByLabel("Summary").press("Enter");
  await expect(page.getByRole("heading", { level: 1, name: "Edited summary" })).toBeVisible();
  await expect.poll(async () => (await a.get(`/issues/${story.key}`)).summary).toBe("Edited summary");

  // Comment.
  await page.getByRole("button", { name: "Add a comment…" }).click();
  await page.locator(".tiptap").last().click();
  await page.keyboard.type("Looks good to me");
  await page.getByRole("button", { name: "Comment", exact: true }).click();
  await expect(page.getByText("Looks good to me")).toBeVisible();

  // Done is refused while the sub-task is open; the validator's message is shown.
  await page.getByRole("button", { name: /^Status: In Progress/ }).click();
  await page.getByRole("menuitem", { name: /^Done/ }).click();
  await expect(page.getByText("Finish every sub-task first.").first()).toBeVisible();
  expect((await a.get(`/issues/${story.key}`)).status.name).toBe("In Progress");
});

test("transition screen: required resolution before closing a bug", async ({ page }) => {
  const { key, a } = await setup(page, "TS", "bug_tracking");
  const bug = await a.post("/issues", { projectKey: key, typeKey: "bug", summary: "Crash on save" });
  for (const t of ["triage", "start-work", "ready-for-qa"]) await a.post(`/issues/${bug.key}/transitions`, { transitionId: t });

  await page.goto(`/browse/${bug.key}`);
  await page.getByRole("button", { name: /^Status: Ready for QA/ }).click();
  await page.getByRole("menuitem", { name: /Pass QA/ }).click();
  const dialog = page.getByRole("dialog", { name: "Pass QA" });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel(/Resolution/).selectOption("Duplicate");
  await dialog.getByRole("button", { name: "Pass QA" }).click();
  await expect(dialog).toBeHidden();
  await expect.poll(async () => (await a.get(`/issues/${bug.key}`)).status.name).toBe("Closed");
  expect((await a.get(`/issues/${bug.key}`)).resolution).toBe("Duplicate");
});

test("board: keyboard drag moves a card to the next column", async ({ page }) => {
  const { key, a } = await setup(page, "BD", "kanban");
  const task = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Drag me across" });

  await page.goto(`/projects/${key}/board`);
  const card = page.getByRole("button", { name: new RegExp(`${task.key}`) }).first();
  await expect(card).toBeVisible();
  await card.focus();
  await page.keyboard.press("Space");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("Space");
  await expect.poll(async () => (await a.get(`/issues/${task.key}`)).status.name, { timeout: 15_000 }).toBe("Selected for Development");
});

test("search: query results and error positions", async ({ page }) => {
  const { key, a } = await setup(page, "SR", "kanban");
  await a.post("/issues", { projectKey: key, typeKey: "bug", summary: "Searchable needle bug" });
  await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Unrelated haystack" });

  await page.goto(`/issues?jql=${encodeURIComponent(`project = ${key} AND text ~ "needle"`)}`);
  await expect(page.getByText("Searchable needle bug")).toBeVisible();
  await expect(page.getByText("Unrelated haystack")).toBeHidden();

  // A broken query from a link opens in the JQL editor with the server's message.
  await page.goto(`/issues?jql=${encodeURIComponent(`project = ${key} AND colour = red`)}`);
  await expect(page.getByLabel("Search query")).toBeVisible();
  await expect(page.getByText(/colour/i).filter({ hasNot: page.getByLabel("Search query") }).last()).toBeVisible();
});

test("workflow editor shows the project's workflow and its YAML", async ({ page }) => {
  const { key } = await setup(page, "WF", "scrum");
  await page.goto(`/projects/${key}/settings/workflows`);
  await page.getByRole("link", { name: "Software development" }).first().click();
  await expect(page.getByText("In Review").first()).toBeVisible();
  await page.getByRole("tab", { name: "YAML" }).click();
  await expect(page.getByText(/name: Software development/).first()).toBeVisible();
});

test("administration: a SCIM connection's token is shown once and works", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const me = await api(page.request).get("/me");
  test.skip(me.orgRole !== "admin", "needs an organisation admin (first user or WFM_BOOTSTRAP_ADMINS)");

  await page.goto("/admin/provisioning");
  await page.getByRole("button", { name: "New connection" }).first().click();
  const create = page.getByRole("dialog", { name: "New SCIM connection" });
  await create.getByLabel("Name").fill(`E2E IdP ${Date.now()}`);
  await create.getByRole("button", { name: "Create" }).click();

  const created = page.getByRole("dialog", { name: /connection created/ });
  await expect(created).toBeVisible();
  const token = (await created.getByText(/wfm_scim_[A-Za-z0-9_-]+/).first().textContent())!.match(/wfm_scim_[A-Za-z0-9_-]+/)![0];
  const res = await page.request.get("/scim/v2/ServiceProviderConfig", { headers: { Authorization: `Bearer ${token}` } });
  expect(res.status()).toBe(200);
});
