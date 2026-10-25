import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Time tracking: estimates on the issue, logging work (with the remaining estimate adjusted,
// set or left), editing and deleting worklogs, the board card field and the timesheet.

const H = 3600;

test("time tracking: estimates, logging work and the timesheet", async ({ page }) => {
  await signIn(page, "e2e-time@example.com", "Tina Timekeeper");
  const a = api(page.request);
  const key = projectKey("TT");
  await a.post("/projects", { key, name: `Time tracking ${key}`, template: "kanban" });
  const issue = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Rebuild the search index", originalEstimate: 8 * H });

  await page.goto(`/browse/${issue.key}`);
  const panel = page.getByRole("region", { name: "Time tracking" });
  await expect(panel.getByRole("button", { name: "Original estimate" })).toHaveText("1d");
  await expect(panel.getByRole("button", { name: "Remaining estimate" })).toHaveText("1d");

  // Log 3h: the remaining estimate adjusts automatically.
  await panel.getByRole("button", { name: "Log work" }).click();
  let dialog = page.getByRole("dialog", { name: `Log work on ${issue.key}` });
  await dialog.getByLabel("Time spent").fill("3h");
  await expect(dialog.getByText("1d → 5h")).toBeVisible();
  await dialog.getByLabel("Work description").fill("Profiled the indexer");
  await dialog.getByRole("button", { name: "Log work" }).click();
  await expect(dialog).toBeHidden();
  await expect(panel.getByText("3h logged")).toBeVisible();
  await expect(panel.getByRole("button", { name: "Remaining estimate" })).toHaveText("5h");

  // Log 6h and set the remaining estimate to 1h: the issue runs over its estimate.
  await panel.getByRole("button", { name: "Log work" }).click();
  dialog = page.getByRole("dialog", { name: `Log work on ${issue.key}` });
  await dialog.getByLabel("Time spent").fill("6h");
  await dialog.getByLabel("New remaining estimate").fill("1h");
  await expect(dialog.getByRole("radio", { name: "Set to" })).toBeChecked();
  await dialog.getByRole("button", { name: "Log work" }).click();
  await expect(dialog).toBeHidden();
  await expect(panel.getByText("1d 1h logged")).toBeVisible();
  await expect(panel.getByText("2h over the 1d estimate")).toBeVisible();

  // Raise the original estimate in place.
  await panel.getByRole("button", { name: "Original estimate" }).click();
  await panel.getByRole("textbox", { name: "Original estimate" }).fill("2d");
  await panel.getByRole("textbox", { name: "Original estimate" }).press("Enter");
  await expect(panel.getByRole("button", { name: "Original estimate" })).toHaveText("2d");
  await expect(panel.getByText(/over the/)).toBeHidden();

  // The work log: edit one entry (the remaining estimate absorbs the difference), delete another.
  await page.getByRole("tab", { name: /Work log/ }).click();
  const log = page.getByRole("list", { name: "Work log" });
  await expect(log.getByRole("listitem")).toHaveCount(2);
  const first = log.getByRole("listitem").filter({ hasText: "Profiled the indexer" });
  await first.getByRole("button", { name: "Edit" }).click();
  dialog = page.getByRole("dialog", { name: "Edit work log" });
  await expect(dialog.getByLabel("Time spent")).toHaveValue("3h");
  await dialog.getByLabel("Time spent").fill("2h");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).toBeHidden();
  await expect(first.getByText("2h", { exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Remaining estimate" })).toHaveText("2h");
  await log.getByRole("listitem").filter({ hasText: "6h" }).getByRole("button", { name: "Delete" }).click();
  await page.getByRole("dialog", { name: "Delete work log?" }).getByRole("button", { name: "Delete" }).click();
  await expect(log.getByRole("listitem")).toHaveCount(1);
  await expect(panel.getByText("2h logged")).toBeVisible();
  await expect(panel.getByRole("button", { name: "Remaining estimate" })).toHaveText("1d");

  await page.getByRole("tab", { name: "History" }).click();
  await expect(page.getByText("logged work").first()).toBeVisible();
  await expect(page.getByText("deleted logged work")).toBeVisible();

  // The issue's time tracking on board cards.
  const board = (await a.get(`/projects/${key}/boards`))[0];
  await page.request.fetch(`/api/v1/boards/${board.id}`, { method: "PUT", headers: { "X-Requested-With": "e2e" }, data: { name: board.name, type: board.type, cardFields: ["timeTracking", "assignee"] } });
  await page.goto(`/projects/${key}/board`);
  await expect(page.locator(`[data-issue-key="${issue.key}"]`).getByLabel("2h logged of 2d estimated, 1d remaining")).toBeVisible();

  // Work logged on another day shows on the timesheet too: the week of yesterday holds today's
  // 2h as well unless today starts a new week (Monday).
  const other = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Write the runbook" });
  const yesterday = new Date();
  yesterday.setDate(yesterday.getDate() - 1);
  yesterday.setHours(11, 0, 0, 0);
  await a.post(`/issues/${other.key}/worklogs`, { timeSpentSeconds: 1.5 * H, startedAt: yesterday.toISOString() });
  const sameWeek = new Date().getDay() !== 1;
  const total = sameWeek ? "3.5" : "1.5";
  const day = `${yesterday.getFullYear()}-${String(yesterday.getMonth() + 1).padStart(2, "0")}-${String(yesterday.getDate()).padStart(2, "0")}`;

  await page.goto(`/timesheet?projects=${key}&from=${day}`);
  const sheet = page.getByRole("table", { name: "Timesheet" });
  const row = sheet.getByRole("row").filter({ has: page.getByRole("button", { name: "Tina Timekeeper" }) });
  await expect(row.getByRole("cell").last()).toHaveText(total);
  await row.getByRole("button", { name: "Tina Timekeeper" }).click();
  await expect(sheet.getByRole("link", { name: other.key })).toBeVisible();
  if (sameWeek) await expect(sheet.getByRole("link", { name: issue.key })).toBeVisible();

  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export CSV" }).click();
  const csv = await readFile((await (await download).path())!, "utf8");
  expect(csv.split("\r\n")[0]).toMatch(/^Person,Email,Issue,Summary,\d{4}-\d{2}-\d{2},/);
  expect(csv).toContain(`Tina Timekeeper,e2e-time@example.com,${other.key},Write the runbook,`);
  expect(csv).toContain(`Everyone,,,Total,`);
  expect(csv.trimEnd().endsWith(`,${total}`)).toBe(true);

  // Filters: back to every project.
  await page.getByRole("combobox", { name: "Project" }).selectOption({ index: 0 });
  await expect(page).not.toHaveURL(/projects=/);
});

test("sprint report in hours", async ({ page }) => {
  await signIn(page, "e2e-time@example.com", "Tina Timekeeper");
  const a = api(page.request);
  const key = projectKey("TH");
  await a.post("/projects", { key, name: `Hours ${key}`, template: "scrum" });
  const sprint = (await a.get(`/projects/${key}/sprints`))[0];
  const issue = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Estimated in hours", originalEstimate: 10 * H, sprintId: sprint.id });
  const today = new Date();
  const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  await a.post(`/sprints/${sprint.id}/start`, { startDate: iso(today), endDate: iso(new Date(today.getTime() + 9 * 86_400_000)) });
  await a.post(`/issues/${issue.key}/worklogs`, { timeSpentSeconds: 4 * H });

  await page.goto(`/projects/${key}/reports`);
  await page.getByRole("radio", { name: "Hours" }).click();
  await expect(page.getByText("Committed", { exact: true }).locator("..")).toContainText("10 hours");
  await expect(page.getByText("Logged", { exact: true }).locator("..")).toContainText("4 hours");
  await expect(page.getByRole("img", { name: /Burndown: 6 of 10 hours remaining/ })).toBeVisible();
});
