import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// The timeline draws bars from issue dates and arrows for "blocks" links; dragging a bar
// reschedules the issue. Week zoom draws a day as 30px.
const DAY_PX = 30;

/** iso returns today's date plus `days` as YYYY-MM-DD (UTC, like the arithmetic below). */
function iso(days: number, from = new Date().toISOString().slice(0, 10)) {
  const d = new Date(`${from}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

test("timeline: bars, a dependency and dragging a bar a week later", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey("TL");
  const a = api(page.request);
  await a.post("/projects", { key, name: `Timeline ${key}`, template: "scrum" });
  const epic = await a.post("/issues", { projectKey: key, typeKey: "epic", summary: "Launch the new flow" });
  const design = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Design the flow", parentKey: epic.key, startDate: iso(1), dueDate: iso(10) });
  const build = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Build the flow", parentKey: epic.key, startDate: iso(14), dueDate: iso(24) });
  await a.post(`/issues/${design.key}/links`, { type: "blocks", targetKey: build.key });

  await page.goto(`/projects/${key}/timeline`);
  await page.getByRole("radio", { name: "Weeks" }).click();
  await page.getByRole("button", { name: "Today" }).click();

  // Both stories have bars; the epic's bar is derived from them; the dependency is drawn.
  const bar = page.locator(`[data-bar-key="${design.key}"]`);
  await expect(bar).toBeVisible();
  await expect(page.locator(`[data-bar-key="${build.key}"]`)).toBeVisible();
  await expect(page.locator(`[data-bar-key="${epic.key}"]`)).toBeVisible();
  const arrow = page.locator("g[data-link-id]").filter({ has: page.getByRole("button", { name: `${design.key} blocks ${build.key}` }) });
  await expect(arrow).toHaveCount(1);
  await expect(arrow).not.toHaveAttribute("data-conflict");

  // Drag the design story a week later.
  const box = (await bar.boundingBox())!;
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width / 2, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 3.5 * DAY_PX, y, { steps: 5 });
  await page.mouse.move(box.x + box.width / 2 + 7 * DAY_PX, y, { steps: 5 });
  await page.mouse.up();

  await expect
    .poll(async () => {
      const i = await a.get(`/issues/${design.key}`);
      return `${i.startDate} ${i.dueDate}`;
    })
    .toBe(`${iso(7, design.startDate)} ${iso(7, design.dueDate)}`);

  // It now ends after the story it blocks starts: the arrow turns into a conflict.
  await expect(arrow).toHaveAttribute("data-conflict", "true");
  await expect(page.getByText("Dependency conflicts").locator("..")).toContainText("1");
});
