import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Automation: create a rule from a template in the gallery, then check that it runs on the
// parent issue (a sub-task finishing its story).
test("automation template: finish the parent when its last sub-task is done", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey("AU");
  const a = api(page.request);
  await a.post("/projects", { key, name: `Automation ${key}`, template: "scrum" });

  await page.goto(`/projects/${key}/settings/automation`);
  await page.getByRole("button", { name: "Use template: Finish the parent when its last sub-task is done" }).click();
  const dialog = page.getByRole("dialog", { name: "New rule from a template" });
  await expect(dialog.getByLabel(/^Name/)).toHaveValue("Finish the parent when its last sub-task is done");
  await expect(dialog.getByLabel("Action 1: apply to")).toHaveValue("parent");
  await dialog.getByRole("button", { name: "Create rule" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText("When a sub-task enters Done and its parent has no open children and its parent is not done yet")).toBeVisible();

  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Story with one sub-task" });
  const sub = await a.post("/issues", { projectKey: key, typeKey: "subtask", summary: "The only sub-task", parentKey: story.key });
  await a.post(`/issues/${story.key}/transitions`, { transitionId: "start-work" });
  await a.post(`/issues/${sub.key}/transitions`, { transitionId: "done" });
  await expect.poll(async () => (await a.get(`/issues/${story.key}`)).status.name, { timeout: 20_000 }).toBe("Done");
  const comments = await a.get(`/issues/${story.key}/comments`);
  expect(comments.map((c: { body: string }) => c.body)).toContain(`All sub-tasks done (${sub.key} was last).`);
});

// Templates naming a status the project's workflows lack are marked, but only once every
// workflow has loaded: a request that failed (a rate limit, say) must not make a status look
// missing.
test("automation templates: a missing status is only reported from complete workflows", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const scrum = projectKey("AUS");
  const kanban = projectKey("AUK");
  await a.post("/projects", { key: scrum, name: `Automation ${scrum}`, template: "scrum" });
  await a.post("/projects", { key: kanban, name: `Automation ${kanban}`, template: "kanban" });
  const template = page.getByRole("button", { name: "Use template: Move to In Review when a pull request opens" });
  const badge = template.getByText("No “In Review” status");

  // The Kanban flow has no In Review.
  await page.goto(`/projects/${kanban}/settings/automation`);
  await expect(badge).toBeVisible();

  // Scrum's Software development has it, even while that workflow can't be loaded.
  const types: { key: string; workflowId: string }[] = await a.get(`/projects/${scrum}/issue-types`);
  const dev = types.find((t) => t.key === "story")!.workflowId;
  const failing = `**/api/v1/workflows/${dev}`;
  await page.route(failing, (route) => route.fulfill({ status: 429, contentType: "application/problem+json", body: '{"title":"Too Many Requests","status":429}' }));
  const refused = page.waitForResponse((r) => r.url().endsWith(`/workflows/${dev}`));
  await page.goto(`/projects/${scrum}/settings/automation`);
  await refused;
  await expect(template).toBeVisible();
  await page.waitForLoadState("networkidle");
  await expect(badge).toHaveCount(0);
  await page.unroute(failing);
  await page.reload();
  await expect(template).toBeVisible();
  await expect(badge).toHaveCount(0);
});
