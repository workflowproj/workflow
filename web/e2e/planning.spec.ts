import { expect, test, type Page } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Planning screens in the browser: how the board lays out and moves cards, why a sprint can't
// start, and which issue keys in Markdown become links. Each test creates its own project.

async function setup(page: Page, prefix: string, template: string) {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey(prefix);
  const a = api(page.request);
  await a.post("/projects", { key, name: `${prefix} ${key}`, template });
  return { key, a };
}

test("board: columns share the width evenly and long summaries wrap", async ({ page }) => {
  const { key, a } = await setup(page, "BW", "kanban");
  const long = "Investigate the intermittent failure in the nightly export where the CSV writer truncates rows with multibyte characters at buffer boundaries";
  const wide = await a.post("/issues", { projectKey: key, typeKey: "task", summary: long });
  const word = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Supercalifragilistic_identifier_without_any_spaces_at_all_in_it_whatsoever" });
  await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Short" });

  await page.goto(`/projects/${key}/board`);
  const columns = page.getByRole("group", { name: / column$/ });
  await expect(columns).toHaveCount(4);
  await expect(page.locator(`[data-issue-key="${wide.key}"]`)).toBeVisible();
  const widths = await columns.evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().width)));
  expect(new Set(widths).size, `column widths ${widths.join(", ")}`).toBe(1);
  // The board fits the 1440px viewport: one long summary doesn't widen anything.
  expect(widths[0]).toBeLessThan(320);
  for (const k of [wide.key, word.key]) {
    const card = await page.locator(`[data-issue-key="${k}"] p`).first().evaluate((p) => ({ scroll: p.scrollWidth, client: p.clientWidth }));
    expect(card.scroll, `${k} summary fits its card`).toBeLessThanOrEqual(card.client);
  }
});

test("board: a move with a transition screen waits for it, and Cancel leaves the card", async ({ page }) => {
  const { key, a } = await setup(page, "SCN", "service");
  const req = await a.post("/issues", { projectKey: key, typeKey: "service_request", summary: "Printer jammed" });

  await page.goto(`/projects/${key}/board`);
  const waiting = page.getByRole("group", { name: "Waiting for support column" }).locator(`[data-issue-key="${req.key}"]`);
  const done = page.getByRole("group", { name: "Done column" }).locator(`[data-issue-key="${req.key}"]`);
  // Drag the card onto Done, dropping once the drag announces it is over that column.
  const drag = async () => {
    const from = (await waiting.boundingBox())!;
    const to = (await page.getByRole("group", { name: "Done column" }).boundingBox())!;
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 3 });
    await expect(page.getByText(`${req.key} is over Waiting for support.`)).toBeAttached();
    await page.mouse.move(to.x + to.width / 2, to.y + 120, { steps: 12 });
    await expect(page.getByText(`${req.key} is over Done.`)).toBeAttached();
    await page.mouse.up();
  };

  await expect(waiting).toBeVisible();
  await drag();
  const dialog = page.getByRole("dialog", { name: "Resolve" });
  await expect(dialog).toBeVisible();
  // Behind the dialog the card is still where it was, not struck through in Done.
  await expect(waiting).toBeVisible();
  await expect(done).toHaveCount(0);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  await expect(waiting).toBeVisible();
  await expect(done).toHaveCount(0);
  expect((await a.get(`/issues/${req.key}`)).status.name).toBe("Waiting for support");

  // Confirming the screen makes the move.
  await drag();
  await expect(dialog).toBeVisible();
  await dialog.getByLabel(/Resolution/).selectOption("Done");
  await dialog.getByRole("button", { name: "Resolve" }).click();
  await expect(dialog).toBeHidden();
  await expect(done).toBeVisible();
  await expect.poll(async () => (await a.get(`/issues/${req.key}`)).status.name).toBe("Resolved");
});

test("backlog: sprint names, counts and why a sprint can't start", async ({ page }) => {
  const { key, a } = await setup(page, "SP", "scrum");
  const [first] = await a.get(`/projects/${key}/sprints`);
  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Pay by card", storyPoints: 3, sprintId: first.id });
  await a.post("/issues", { projectKey: key, typeKey: "subtask", summary: "Card form", parentKey: story.key });
  await a.patch(`/sprints/${first.id}`, { name: "Sprint 23" });

  await page.goto(`/projects/${key}/backlog`);
  await page.getByRole("button", { name: "Create sprint" }).click();
  await expect(page.getByRole("region", { name: "Sprint 24" })).toBeVisible();

  // The dialog counts like the backlog: the story, not its sub-task.
  await page.getByRole("region", { name: "Sprint 23" }).getByRole("button", { name: "Start sprint" }).click();
  const dialog = page.getByRole("dialog", { name: "Start sprint" });
  await expect(dialog.getByText(/^1 issue and 3 story points will be included/)).toBeVisible();
  const startDate = await dialog.getByLabel("Start date").inputValue();
  const before = new Date(Date.parse(startDate) - 86_400_000).toISOString().slice(0, 10);
  await dialog.getByLabel("End date").fill(before);
  await expect(dialog.getByText("The end date must be after the start date.")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Start sprint" })).toBeDisabled();
  await dialog.getByLabel("Duration").selectOption("2");
  await expect(dialog.getByText("The end date must be after the start date.")).toBeHidden();
  await dialog.getByRole("button", { name: "Start sprint" }).click();
  await expect(dialog).toBeHidden();

  // With Sprint 23 active, Sprint 24 says why it can't start.
  const next = page.getByRole("region", { name: "Sprint 24" });
  await expect(next.getByText("Complete Sprint 23 first")).toBeVisible();
  await expect(next.getByRole("button", { name: "Start sprint" })).toBeDisabled();
});

test("markdown: only keys of projects you can see link to issues", async ({ page }) => {
  const { key, a } = await setup(page, "MD", "kanban");
  const target = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Linked" });
  const issue = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Mentions", description: `Due by DS-2026 under ISO-9001; see ${target.key}.` });

  await page.goto(`/browse/${issue.key}`);
  const body = page.getByText(/Due by DS-2026/);
  await expect(body).toBeVisible();
  await expect(body.getByRole("link", { name: target.key })).toHaveAttribute("href", `/browse/${target.key}`);
  await expect(body.getByRole("link")).toHaveCount(1);
});
