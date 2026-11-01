import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// A programme plans across team projects: teams are added in its settings, a team's epic joins an
// initiative from its Initiative field, and the roadmap, timeline and dependencies follow.
test("programmes: teams, an epic under an initiative, the roadmap, timeline and dependencies", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const prog = projectKey("PG");
  const app = projectKey("PGA");
  const ops = projectKey("PGO");
  await a.post("/projects", { key: prog, name: `Checkout ${prog}`, template: "programme" });
  await a.post("/projects", { key: app, name: `App ${app}`, template: "scrum" });
  await a.post("/projects", { key: ops, name: `Ops ${ops}`, template: "kanban" });
  const initiative = await a.post("/issues", { projectKey: prog, typeKey: "initiative", summary: "Self-serve checkout", dueDate: "2026-12-18" });
  const epic = await a.post("/issues", { projectKey: app, typeKey: "epic", summary: "Card payments" });
  const story = await a.post("/issues", { projectKey: app, typeKey: "story", summary: "Card form", parentKey: epic.key, storyPoints: 3 });
  const opsEpic = await a.post("/issues", { projectKey: ops, typeKey: "epic", summary: "Payment gateway" });
  const opsTask = await a.post("/issues", { projectKey: ops, typeKey: "task", summary: "Open the firewall", parentKey: opsEpic.key });
  await a.post(`/issues/${opsTask.key}/links`, { type: "blocks", targetKey: story.key });

  // The programme opens on its roadmap; with no teams yet it says how to add them.
  await page.goto(`/projects/${prog}`);
  await expect(page).toHaveURL(new RegExp(`/projects/${prog}/roadmap`));
  await expect(page.getByText("No teams yet")).toBeVisible();

  // Add both teams in settings.
  await page.goto(`/projects/${prog}/settings/teams`);
  for (const key of [app, ops]) {
    await page.getByLabel("Add a team project").selectOption(key);
    await page.getByRole("button", { name: "Add team" }).click();
    await expect(page.getByRole("list", { name: "Team projects" }).getByRole("link", { name: new RegExp(key) })).toBeVisible();
  }

  // The App epic joins the initiative from its Initiative field.
  await page.goto(`/browse/${epic.key}`);
  await page.getByRole("button", { name: "Initiative" }).click();
  await page.getByPlaceholder("Search initiatives…").fill(initiative.key);
  await page.getByRole("option", { name: new RegExp(initiative.key) }).click();
  await expect(page.getByRole("button", { name: "Initiative" })).toContainText(initiative.key);

  // The roadmap shows the initiative with the epic under the App team, its story counted, and the
  // Ops epic waiting under no initiative.
  await page.goto(`/projects/${prog}/roadmap`);
  const card = page.locator(`[data-initiative="${initiative.key}"]`);
  await expect(card).toContainText("Self-serve checkout");
  await expect(card.locator(`[data-epic="${epic.key}"]`)).toContainText("Card payments");
  await expect(card.getByRole("progressbar").first()).toHaveAttribute("aria-valuetext", "0 of 1 done · 0 of 3 pts");
  await expect(card.locator(`[data-epic="${epic.key}"]`)).toContainText("Blocked");
  const unlinked = page.locator(`[data-unlinked="${opsEpic.key}"]`);
  await expect(unlinked).toBeVisible();

  // Link the Ops epic from the roadmap.
  await unlinked.getByLabel(`Initiative for ${opsEpic.key}`).selectOption(initiative.key);
  await expect(card.locator(`[data-epic="${opsEpic.key}"]`)).toBeVisible();
  await expect(page.locator(`[data-unlinked="${opsEpic.key}"]`)).toHaveCount(0);

  // The timeline nests the initiative above both teams' epics.
  await page.goto(`/projects/${prog}/timeline`);
  const rows = page.locator("[data-row-key]");
  await expect(rows.first()).toHaveAttribute("data-row-key", initiative.key);
  const order = await rows.evaluateAll((els) => els.map((e) => e.getAttribute("data-row-key")));
  expect(order.indexOf(initiative.key)).toBeLessThan(order.indexOf(epic.key));
  expect(order.indexOf(epic.key)).toBeLessThan(order.indexOf(story.key));
  expect(order).toContain(opsEpic.key);

  // Dependencies: Ops blocks App once.
  await page.goto(`/projects/${prog}/dependencies`);
  await expect(page.locator(`[data-cell="${ops}>${app}"]`)).toHaveText("1");
  await expect(page.getByRole("table").nth(1)).toContainText(opsTask.key);
});
