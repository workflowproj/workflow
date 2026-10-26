import { expect, test } from "@playwright/test";
import { api, projectKey, run, signIn } from "./helpers";

// Releases end to end: the release page (progress, readiness, burnup, issues), release notes
// generated, edited, saved, downloaded and published, shipping with the readiness warnings in
// front of you, and the help centre's What's new page as a customer reads it.

const isoDay = (offset: number) => {
  const d = new Date();
  d.setDate(d.getDate() + offset);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};

test("release page: progress, readiness, notes and shipping", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const key = projectKey("REL");
  await a.post("/projects", { key, name: `Releases ${key}`, template: "kanban" });
  const rel = await a.post(`/projects/${key}/releases`, { name: "3.9.0", description: "The checkout release", releaseDate: isoDay(-1) });
  await a.post(`/projects/${key}/releases`, { name: "4.0.0" });
  const statuses: { id: string; name: string }[] = await a.get("/statuses");
  const done = statuses.find((s) => s.name === "Done")!.id;
  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Pay by card", releaseIds: [rel.id], storyPoints: 3 });
  const bug = await a.post("/issues", { projectKey: key, typeKey: "bug", summary: "Totals are off by one", releaseIds: [rel.id], storyPoints: 2 });
  const task = await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Faster search", releaseIds: [rel.id] });
  for (const i of [story, bug]) await a.post(`/issues/${i.key}/move`, { statusId: done });

  // The Releases list links to the page.
  await page.goto(`/projects/${key}/releases`);
  await page.getByRole("link", { name: "3.9.0", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${key}/releases/${rel.id}`));
  await expect(page.getByRole("heading", { name: /3\.9\.0/ })).toBeVisible();
  await expect(page.getByText("The checkout release")).toBeVisible();
  await expect(page.getByText("1 day late")).toBeVisible();

  // Progress, readiness and the burnup.
  await expect(page.getByText(/2 of 3 done/)).toBeVisible();
  await expect(page.getByText(/5 of 5 done/)).toBeVisible();
  await expect(page.getByText("1 issue has no story points.")).toBeVisible();
  const readiness = page.locator("section", { has: page.getByRole("heading", { name: "Readiness" }) });
  await expect(readiness.getByText("1 issue is not done.")).toBeVisible();
  await expect(readiness.getByText(/The release date, .* has passed/)).toBeVisible();
  await expect(page.getByRole("img", { name: /Burnup: 2 of 3 issues done|Burnup: 5 of 5 pts done/ })).toBeVisible();
  await expect(page.getByText(/scope is the release's current issues/)).toBeVisible();

  // Issues, filterable by status category.
  const issues = page.locator("section", { has: page.getByRole("heading", { name: "Issues", exact: true }) });
  await issues.getByRole("radio", { name: /Done/ }).click();
  await expect(issues.getByText(task.key)).toHaveCount(0);
  await expect(issues.getByText(story.key)).toBeVisible();

  // Release notes: generate, save, publish, download.
  await page.getByRole("button", { name: "Generate" }).click();
  const notes = page.getByRole("textbox", { name: "Release notes (Markdown)" });
  await expect(notes).toHaveValue(`## Features\n\n- Pay by card (${story.key})\n\n## Bug fixes\n\n- Totals are off by one (${bug.key})\n`);
  await notes.fill((await notes.inputValue()) + "\nThanks to everyone who tested it.\n");
  await page.getByRole("button", { name: "Save notes" }).click();
  await expect(page.getByText("Release notes saved")).toBeVisible();
  // Generating again would overwrite the edits: it asks first.
  await page.getByRole("button", { name: "Generate" }).click();
  const confirm = page.getByRole("dialog", { name: "Replace the release notes?" });
  await expect(confirm).toBeVisible();
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(notes).toHaveValue(/Thanks to everyone who tested it\./);
  await page.getByRole("switch", { name: "Show on the help centres' What's new" }).click();
  await expect(page.getByText("Release notes published to the help centres")).toBeVisible();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download .md" }).click();
  expect((await download).suggestedFilename()).toBe("release-notes-3.9.0.md");
  await page.getByRole("radio", { name: "Preview" }).click();
  await expect(page.getByRole("heading", { name: "Bug fixes" })).toBeVisible();

  // Shipping shows the warnings first, and stays possible.
  await page.getByRole("button", { name: "Release", exact: true }).click();
  const ship = page.getByRole("dialog", { name: "Release 3.9.0" });
  await expect(ship.getByText(`1 issue is not done. (${task.key})`)).toBeVisible();
  await ship.getByRole("combobox", { name: /unfinished issue/ }).selectOption({ label: "Move them to 4.0.0" });
  await ship.getByRole("button", { name: "Release anyway" }).click();
  await expect(page.getByText("3.9.0 released")).toBeVisible();
  await expect(page.getByRole("heading", { name: /3\.9\.0/ }).getByText("Released")).toBeVisible();
  await expect(page.getByRole("button", { name: "Release", exact: true })).toHaveCount(0);
});

test("help centre: What's new shows the release notes a desk chose", async ({ page, browser }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const app = projectKey("APP");
  const sd = projectKey("WNSD");
  await a.post("/projects", { key: app, name: `Shop app ${app}`, template: "kanban" });
  await a.post("/projects", { key: sd, name: `Help ${sd}`, template: "service" });
  await a.post(`/projects/${app}/releases`, { name: "2.1", releaseDate: isoDay(-3), notes: "## Features\n\n- Gift cards at checkout", notesPublished: true });
  await a.post(`/projects/${app}/releases`, { name: "2.2", notes: "## Features\n\n- Not announced yet", notesPublished: false });

  // The desk's admin chooses the project in Help centre settings.
  await page.goto(`/projects/${sd}/settings/portal`);
  const section = page.locator("section", { has: page.getByRole("heading", { name: "What's new" }) });
  await section.getByLabel("Show release notes of").selectOption({ label: `Shop app ${app} (${app})` });
  await section.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("What's new saved")).toBeVisible();
  await expect(section.getByRole("link", { name: `Shop app ${app}` })).toBeVisible();

  // A customer of the desk reads them in the help centre.
  const email = `e2e-wn-${run.toLowerCase()}@example.com`;
  await a.post(`/projects/${sd}/customers`, { email, displayName: `Wendy ${run}` });
  const ctx = await browser.newContext();
  const customer = await ctx.newPage();
  await customer.goto("/login");
  await customer.getByLabel("Email").fill(email);
  await customer.getByLabel("Display name").fill(`Wendy ${run}`);
  await customer.getByRole("button", { name: "Sign in" }).click();
  await expect(customer).toHaveURL(/\/portal/);
  await customer.goto(`/portal/${sd}`);
  await customer.getByRole("link", { name: "What's new" }).click();
  await expect(customer).toHaveURL(new RegExp(`/portal/${sd}/whats-new`));
  await expect(customer.getByRole("heading", { name: `Shop app ${app} 2.1` })).toBeVisible();
  await expect(customer.getByText("Gift cards at checkout")).toBeVisible();
  await expect(customer.getByText("Not announced yet")).toHaveCount(0);
  await ctx.close();
});
