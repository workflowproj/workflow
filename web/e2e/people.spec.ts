import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// The people filter: assignee avatars above the board and the backlog narrow them to the
// issues of the people picked (several at once, "Unassigned" included).

const today = new Date().toISOString().slice(0, 10);
const inTwoWeeks = new Date(Date.now() + 14 * 86_400_000).toISOString().slice(0, 10);

test("people filter: board and backlog show only the picked assignees", async ({ page, browser }) => {
  // Teammates sign in once so they exist and can be assigned.
  const mates: { id: string }[] = [];
  for (const [email, name] of [
    ["e2e-people-ada@example.com", "Ada People"],
    ["e2e-people-bob@example.com", "Bob People"],
  ]) {
    const ctx = await browser.newContext();
    const p = await ctx.newPage();
    await signIn(p, email, name);
    mates.push(await api(p.request).get("/me"));
    await ctx.close();
  }
  const [ada, bob] = mates;

  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const key = projectKey("PF");
  await a.post("/projects", { key, name: `People ${key}`, template: "scrum" });
  const sprint = await a.post(`/projects/${key}/sprints`, {});
  const create = (summary: string, assigneeId?: string, sprintId: string | undefined = sprint.id) => a.post("/issues", { projectKey: key, typeKey: "story", summary, assigneeId, sprintId });
  await create("Ada first", ada.id);
  await create("Ada second", ada.id);
  await create("Bob in sprint", bob.id);
  await create("Nobody yet");
  await create("Bob in backlog", bob.id, undefined);
  await a.post(`/sprints/${sprint.id}/start`, { startDate: today, endDate: inTwoWeeks });

  // Board: one avatar per assignee of the sprint's issues, plus Unassigned.
  await page.goto(`/projects/${key}/board`);
  const people = page.getByRole("group", { name: "Filter by assignee" });
  await expect(people.getByRole("button")).toHaveCount(3);
  await expect(people.getByRole("button").first()).toHaveAccessibleName("Ada People: 2 issues");

  await people.getByRole("button", { name: "Ada People: 2 issues" }).click();
  await expect(people.getByRole("button", { name: "Ada People: 2 issues" })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByText("Ada first")).toBeVisible();
  await expect(page.getByText("Ada second")).toBeVisible();
  await expect(page.getByText("Bob in sprint")).toHaveCount(0);
  await expect(page.getByText("Nobody yet")).toHaveCount(0);

  await people.getByRole("button", { name: "Unassigned: 1 issue" }).click();
  await expect(page.getByText("Nobody yet")).toBeVisible();
  await expect(page.getByText("Bob in sprint")).toHaveCount(0);

  await page.getByRole("button", { name: "Clear filters" }).click();
  await expect(page.getByText("Bob in sprint")).toBeVisible();
  await expect(people.getByRole("button", { pressed: true })).toHaveCount(0);

  // Backlog: the sprint and the backlog list both narrow.
  await page.goto(`/projects/${key}/backlog`);
  await people.getByRole("button", { name: "Bob People: 2 issues" }).click();
  await expect(page.getByText("Bob in sprint")).toBeVisible();
  await expect(page.getByText("Bob in backlog")).toBeVisible();
  await expect(page.getByText("Ada first")).toHaveCount(0);
  await expect(page.getByText("Nobody yet")).toHaveCount(0);
});
