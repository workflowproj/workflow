import { expect, test, type Page } from "@playwright/test";
import { api, projectKey, run, signIn } from "./helpers";

// Dashboards: create one from the sprint template, check its gadgets show the sprint, change a
// gadget, rearrange (keyboard drag and the gadget menu), save; another person sees the shared
// dashboard read-only and can copy it.

const day = (offset: number) => new Date(Date.now() + offset * 86_400_000).toISOString().slice(0, 10);
const gadgetOrder = (page: Page) => page.locator("[data-gadget-id]").evaluateAll((els) => els.map((e) => e.getAttribute("data-gadget-id")));

test("dashboards: template, gadgets, configure, reorder, save, share and copy", async ({ page, browser }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const key = projectKey("DB");
  await a.post("/projects", { key, name: `Dashboards ${key}`, template: "scrum", access: "open" });

  // A running sprint: 18 points, one story done, one in progress, one overdue.
  const sprint = await a.post(`/projects/${key}/sprints`, {});
  const issues: { key: string }[] = [];
  for (const [summary, points, dueDate] of [
    ["Write the spec", 3],
    ["Build the API", 5],
    ["Ship the UI", 8],
    ["Fix the flaky test", 2, day(-2)],
  ] as const) {
    issues.push(await a.post("/issues", { projectKey: key, typeKey: "story", summary, storyPoints: points, sprintId: sprint.id, dueDate }));
  }
  await a.post(`/sprints/${sprint.id}/start`, { startDate: day(-3), endDate: day(10) });
  const moveTo = async (issueKey: string, status: string) => {
    const ts: { id: string; to: { name: string } }[] = await a.get(`/issues/${issueKey}/transitions`);
    await a.post(`/issues/${issueKey}/transitions`, { transitionId: ts.find((t) => t.to.name === status)!.id });
  };
  await moveTo(issues[0].key, "In Progress");
  await moveTo(issues[0].key, "Done");
  await moveTo(issues[1].key, "In Progress");

  // Create a dashboard from the sprint template.
  await page.goto("/dashboards");
  await page.getByRole("button", { name: "New dashboard" }).click();
  const dialog = page.getByRole("dialog", { name: "New dashboard" });
  await dialog.getByRole("radio", { name: /Sprint dashboard/ }).click();
  await dialog.getByLabel("Project").selectOption(key);
  const name = `Sprint room ${run}`;
  await dialog.getByLabel("Name").fill(name);
  await dialog.getByRole("button", { name: "Create dashboard" }).click();
  await expect(page).toHaveURL(/\/dashboards\/[0-9a-f-]{36}$/);
  const url = page.url();
  await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();

  // The gadgets follow the active sprint.
  const health = page.getByRole("region", { name: `Sprint health · ${sprint.name}` });
  await expect(health).toContainText("17%");
  await expect(health).toContainText("3 of 18 pts");
  await expect(page.getByRole("region", { name: `Burndown · ${sprint.name}` }).getByRole("img", { name: /^Burndown: 15 of 18 pts remaining/ })).toBeVisible();
  await expect(page.getByRole("region", { name: `Burnup · ${sprint.name}` }).getByRole("img", { name: /^Burnup: 3 of 18 pts completed/ })).toBeVisible();
  await expect(page.getByRole("region", { name: `Velocity · ${key}` })).toContainText(`No completed sprints in ${key} yet`);
  const assignees = page.getByRole("region", { name: "Open sprint work by assignee" });
  await expect(assignees.getByRole("link", { name: /^E2E Admin: 1 issues/ })).toBeVisible();
  await expect(assignees.getByRole("link", { name: /^Unassigned: 2 issues/ })).toBeVisible();

  // The at-risk list has the overdue story; its row opens the issue drawer.
  const atRisk = page.getByRole("region", { name: "Overdue or blocked" });
  await expect(atRisk.getByRole("row")).toHaveCount(2);
  await atRisk.getByText("Fix the flaky test").click();
  await expect(page.getByRole("dialog", { name: `Issue ${issues[3].key}` })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: `Issue ${issues[3].key}` })).toBeHidden();

  // Edit: regroup the assignee breakdown by priority under its derived title.
  await page.getByRole("button", { name: "Edit" }).click();
  await page.getByRole("button", { name: "Configure Open sprint work by assignee" }).click();
  const settings = page.getByRole("dialog", { name: "Breakdown settings" });
  await settings.getByLabel("Title").fill("");
  await settings.getByLabel("Group by").selectOption("priority");
  await settings.getByRole("button", { name: "Apply" }).click();
  await expect(settings).toBeHidden();
  await expect(page.getByRole("region", { name: `Issues by priority · ${key}` }).getByRole("link", { name: /^Medium: 3 issues/ })).toBeVisible();

  // Rearrange: lift velocity with the keyboard and drop it over the burndown above it, then
  // move the issue list earlier from its menu.
  expect(await gadgetOrder(page)).toEqual(["health", "burndown", "burnup", "velocity", "assignees", "at-risk"]);
  const announcement = page.locator('[id^="DndLiveRegion"]');
  await page.getByRole("button", { name: `Move Velocity · ${key}` }).focus();
  await page.keyboard.press("Space");
  await expect(announcement).toContainText(`Velocity · ${key} is over position 4 of 6`);
  await page.keyboard.press("ArrowUp");
  await expect(announcement).toContainText(`Velocity · ${key} is over position 2 of 6`);
  await page.keyboard.press("Space");
  await expect(announcement).toContainText(`Velocity · ${key} was dropped at position 2 of 6`);
  await expect.poll(() => gadgetOrder(page)).toEqual(["health", "velocity", "burndown", "burnup", "assignees", "at-risk"]);
  await page.getByRole("button", { name: "More actions for Overdue or blocked" }).click();
  await page.getByRole("menuitem", { name: "Move earlier" }).click();
  await expect.poll(() => gadgetOrder(page)).toEqual(["health", "velocity", "burndown", "burnup", "at-risk", "assignees"]);

  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Dashboard saved")).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit" })).toBeVisible();

  // It persists.
  await page.reload();
  await expect(page.getByRole("region", { name: `Issues by priority · ${key}` })).toBeVisible();
  expect(await gadgetOrder(page)).toEqual(["health", "velocity", "burndown", "burnup", "at-risk", "assignees"]);

  // Someone else sees the shared dashboard read-only, and can make their own copy.
  const ctx = await browser.newContext();
  const other = await ctx.newPage();
  await signIn(other, "e2e-dash-viewer@example.com", "Dash Viewer");
  await other.goto(url);
  await expect(other.getByRole("heading", { level: 1, name })).toBeVisible();
  await expect(other.getByText("Owned by E2E Admin")).toBeVisible();
  await expect(other.getByText("View only")).toBeVisible();
  await expect(other.getByRole("button", { name: "Edit" })).toHaveCount(0);
  await expect(other.getByRole("region", { name: `Sprint health · ${sprint.name}` })).toContainText("17%");
  await other.getByRole("button", { name: "Make a copy" }).click();
  await expect(other.getByRole("heading", { level: 1, name: `Copy of ${name}` })).toBeVisible();
  await expect(other).not.toHaveURL(url);
  await expect(other.getByText("Your dashboard")).toBeVisible();
  await expect(other.getByRole("button", { name: "Edit" })).toBeVisible();
  expect(await gadgetOrder(other)).toEqual(["health", "velocity", "burndown", "burnup", "at-risk", "assignees"]);
  await ctx.close();
});
