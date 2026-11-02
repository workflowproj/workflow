import { expect, test } from "@playwright/test";
import { api, projectKey, run, signIn } from "./helpers";

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

test("saved timelines: choose projects, save, reopen, switch, edit and delete", async ({ page, browser }) => {
  // The admin makes two projects with scheduled work.
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const [one, two] = [projectKey("STA"), projectKey("STB")];
  const bars: Record<string, string> = {};
  for (const key of [one, two]) {
    await a.post("/projects", { key, name: `Saved ${key}`, template: "scrum" });
    bars[key] = (await a.post("/issues", { projectKey: key, typeKey: "story", summary: `Ship ${key}`, startDate: iso(1), dueDate: iso(8) })).key;
  }
  const bar = (p: typeof page, key: string) => p.locator(`[data-bar-key="${bars[key]}"]`);

  // Someone new has no timelines yet: the page asks which projects to plan together.
  const ctx = await browser.newContext();
  const p = await ctx.newPage();
  await signIn(p, `e2e-timelines-${run.toLowerCase()}@example.com`, "Tess Timelines");
  await p.goto("/timeline");
  await expect(p.getByText("Timelines show a group of projects together")).toBeVisible();
  await p.getByRole("button", { name: "Choose projects" }).click();
  await p.getByRole("menuitemcheckbox", { name: new RegExp(`Saved ${one}`) }).click();
  await expect(p).toHaveURL(new RegExp(`projects=${one}`));
  await expect(bar(p, one)).toHaveCount(1);
  await expect(bar(p, two)).toHaveCount(0);

  // Save the choice, adding the second project.
  await p.getByRole("button", { name: "Save as timeline" }).click();
  const create = p.getByRole("dialog", { name: "New timeline" });
  await expect(create.getByRole("checkbox", { name: new RegExp(`Saved ${one}`) })).toBeChecked();
  await create.getByRole("checkbox", { name: new RegExp(`Saved ${two}`) }).check();
  await create.getByLabel(/^Name/).fill("Delivery");
  await create.getByRole("button", { name: "Create timeline" }).click();
  await expect(p.getByRole("heading", { level: 1, name: "Delivery" })).toBeVisible();
  await expect(p).toHaveURL(/view=/);
  await expect(bar(p, one)).toHaveCount(1);
  await expect(bar(p, two)).toHaveCount(1);

  // Coming back to the Timeline opens it again.
  await p.goto("/timeline");
  await expect(p.getByRole("heading", { level: 1, name: "Delivery" })).toBeVisible();
  await expect(p).toHaveURL(/view=/);

  // A second timeline, from the switcher.
  await p.getByRole("button", { name: "Delivery", exact: true }).click();
  await p.getByRole("menuitem", { name: "New timeline…" }).click();
  const second = p.getByRole("dialog", { name: "New timeline" });
  await second.getByRole("checkbox", { name: new RegExp(`Saved ${two}`) }).check();
  await second.getByLabel(/^Name/).fill("Second team");
  await second.getByRole("button", { name: "Create timeline" }).click();
  await expect(p.getByRole("heading", { level: 1, name: "Second team" })).toBeVisible();
  await expect(bar(p, one)).toHaveCount(0);
  await expect(bar(p, two)).toHaveCount(1);

  // Switch back, and take the first project out.
  await p.getByRole("button", { name: "Second team", exact: true }).click();
  await p.getByRole("menuitemradio", { name: /Delivery/ }).click();
  await expect(p.getByRole("heading", { level: 1, name: "Delivery" })).toBeVisible();
  await p.getByRole("button", { name: "Edit", exact: true }).click();
  const edit = p.getByRole("dialog", { name: "Edit timeline" });
  await edit.getByRole("checkbox", { name: new RegExp(`Saved ${one}`) }).uncheck();
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toBeHidden();
  await expect(bar(p, one)).toHaveCount(0);
  await expect(bar(p, two)).toHaveCount(1);

  // Delete it: the Timeline opens the one that's left.
  await p.getByRole("button", { name: "Delete", exact: true }).click();
  await p.getByRole("dialog", { name: "Delete timeline?" }).getByRole("button", { name: "Delete" }).click();
  await expect(p.getByRole("heading", { level: 1, name: "Second team" })).toBeVisible();
  const left: { name: string }[] = await api(p.request).get("/timeline-views");
  expect(left.map((v) => v.name)).toEqual(["Second team"]);
  await ctx.close();
});
