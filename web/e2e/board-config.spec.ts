import { expect, test, type Page } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Configuring a board from the board itself (column menus, the configure drawer, the
// unmapped-status notice) and the issue view's metrics.

const inThreeDays = new Date(Date.now() + 3 * 86_400_000).toISOString().slice(0, 10);

async function setup(page: Page, prefix: string) {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey(prefix);
  const a = api(page.request);
  await a.post("/projects", { key, name: `Board config ${key}`, template: "kanban" });
  // Runs a transition by its target status's name.
  const move = async (issueKey: string, to: string) => {
    const list: { id: string; to: { name: string } }[] = await a.get(`/issues/${issueKey}/transitions`);
    const t = list.find((x) => x.to.name === to);
    if (!t) throw new Error(`${issueKey} has no transition to ${to}`);
    await a.post(`/issues/${issueKey}/transitions`, { transitionId: t.id });
  };
  return { key, a, move };
}

async function columnMenu(page: Page, column: string, item: string | RegExp) {
  await page.getByRole("button", { name: `Column actions for ${column}` }).click();
  await page.getByRole("menuitem", { name: item }).click();
}

test("board configuration: columns, WIP limits and card fields from the board", async ({ page }) => {
  const { key, a, move } = await setup(page, "BC");
  const ready = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Ready to pick up", priority: "high", dueDate: inThreeDays });
  await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Still in the backlog", priority: "high" });
  await move(ready.key, "Selected for Development");
  const boardId = (await a.get(`/projects/${key}/boards`))[0].id;
  const savedColumns = async () => ((await a.get(`/projects/${key}/boards`))[0].columns as { name: string; wipLimit?: number }[]).map((c) => c.name + (c.wipLimit ? `/${c.wipLimit}` : ""));

  await page.goto(`/projects/${key}/board`);
  const column = (name: string) => page.getByRole("group", { name: `${name} column`, exact: true });
  await expect(column("Selected for Development").getByText("Ready to pick up")).toBeVisible();

  // Rename from the column header menu.
  await columnMenu(page, "Backlog", "Rename");
  await page.getByLabel("Column name").fill("Inbox");
  await page.getByLabel("Column name").press("Enter");
  await expect(column("Inbox").getByText("Still in the backlog")).toBeVisible();
  await expect.poll(savedColumns).toEqual(["Inbox", "Selected for Development", "In Progress/5", "Done"]);

  // Add a column after another, then map a status into it in the configure drawer: the card moves.
  await columnMenu(page, "Selected for Development", "Add column after");
  await page.getByLabel(/New column after/).fill("Ready");
  await page.getByRole("button", { name: "Add column" }).click();
  await expect(column("Ready")).toBeVisible();
  await expect.poll(savedColumns).toEqual(["Inbox", "Selected for Development", "Ready", "In Progress/5", "Done"]);

  await columnMenu(page, "Ready", "Edit statuses…");
  const drawer = page.getByRole("dialog", { name: "Configure board" });
  await drawer.getByLabel("Add a status to Ready").selectOption({ label: "Selected for Development" });
  await drawer.getByRole("button", { name: "Save changes" }).click();
  await expect(drawer).toBeHidden();
  await expect(column("Ready").getByText("Ready to pick up")).toBeVisible();
  await expect(column("Selected for Development").getByText("Ready to pick up")).toHaveCount(0);

  // The emptied column goes (no confirmation: it has no statuses left).
  await columnMenu(page, "Selected for Development", "Delete column");
  await expect(column("Selected for Development")).toHaveCount(0);
  await expect(page.getByText("Deleted the Selected for Development column")).toBeVisible();

  // WIP limit.
  await columnMenu(page, "Ready", "Set WIP limit");
  await page.getByLabel("WIP limit for Ready").fill("1");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByTitle("Work-in-progress limit: 1")).toHaveText("1 / 1");
  await expect.poll(savedColumns).toEqual(["Inbox", "Ready/1", "In Progress/5", "Done"]);

  // Card layout: drop priority, add the due date and days in column.
  const card = page.locator(`[data-issue-key="${ready.key}"]`);
  await expect(card.getByRole("img", { name: "High priority" })).toBeVisible();
  await page.getByRole("button", { name: "Configure board" }).click();
  await drawer.getByRole("tab", { name: "Card layout" }).click();
  await drawer.getByRole("checkbox", { name: /^Priority/ }).uncheck();
  await drawer.getByRole("checkbox", { name: /^Due date/ }).check();
  await drawer.getByRole("checkbox", { name: /^Days in column/ }).check();
  // The preview follows along before saving.
  await expect(drawer.getByRole("group", { name: "Card preview" }).getByLabel(/^Due /)).toBeVisible();
  await drawer.getByRole("button", { name: "Save changes" }).click();
  await expect(drawer).toBeHidden();
  await expect(card.getByRole("img", { name: "High priority" })).toHaveCount(0);
  await expect(card.getByLabel(/^Due /)).toBeVisible();
  await expect(card.getByLabel("In Selected for Development for less than a day")).toBeVisible();
  const saved = await a.get(`/projects/${key}/boards`);
  expect(saved[0].cardFields).toEqual(["epic", "labels", "dev", "points", "assignee", "dueDate", "daysInColumn"]);

  // Everything survives a reload.
  await page.reload();
  await expect(column("Inbox")).toBeVisible();
  await expect(page.getByTitle("Work-in-progress limit: 1")).toHaveText("1 / 1");
  await expect(card.getByLabel(/^Due /)).toBeVisible();
  await expect(card.getByRole("img", { name: "High priority" })).toHaveCount(0);

  // A status taken off the board is flagged, and can be mapped again from the notice.
  const res = await page.request.put(`/api/v1/boards/${boardId}`, {
    headers: { "X-Requested-With": "e2e" },
    data: { ...saved[0], columns: saved[0].columns.map((c: { name: string; statusIds: string[] }) => (c.name === "Ready" ? { ...c, statusIds: [] } : c)) },
  });
  expect(res.ok()).toBe(true);
  await page.reload();
  await expect(page.getByText(/Issues in Selected for Development aren't shown/)).toBeVisible();
  await expect(page.getByText("Ready to pick up")).toHaveCount(0);
  await page.getByRole("button", { name: "Map status" }).click();
  await page.getByLabel("Column for Selected for Development").selectOption({ label: "Ready" });
  await expect(column("Ready").getByText("Ready to pick up")).toBeVisible();
  await expect(page.getByText(/aren't shown/)).toHaveCount(0);
});

test("issue metrics: lead time, cycle time and time in each status", async ({ page }) => {
  const { key, a, move } = await setup(page, "IM");
  const issue = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Measure me" });
  for (const to of ["Selected for Development", "In Progress", "Done", "In Progress"]) await move(issue.key, to);

  await page.goto(`/browse/${issue.key}`);
  const metrics = page.getByRole("region", { name: "Metrics" });
  await expect(metrics.getByText("Time in status")).toBeVisible();
  const statuses = metrics.getByRole("list");
  await expect(statuses.getByRole("listitem")).toHaveText([/^Backlog/, /^Selected for Development/, /^In Progress.*now.*×2/, /^Done/]);
  // Reopened and in progress again: cycle time counts on, no lead time yet.
  await expect(metrics.getByText("Reopened once")).toBeVisible();
  await expect(metrics.getByText("In progress for", { exact: true })).toBeVisible();
  await expect(metrics.getByRole("definition").first()).toHaveText("—");

  // Resolving it gives a lead time; the panel follows the issue.
  await page.getByRole("button", { name: /^Status: In Progress/ }).click();
  await page.getByRole("menuitem", { name: /^Done/ }).click();
  await expect(metrics.getByText("Cycle time", { exact: true })).toBeVisible();
  await expect(metrics.getByRole("definition").first()).not.toHaveText("—");
  await expect(metrics.getByText("until resolved")).toBeVisible();
});
