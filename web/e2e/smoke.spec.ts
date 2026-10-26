import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

test("sign in, create a project from a template and open an issue", async ({ page }) => {
  // No page asks for a project without a key (GET /api/v1/projects/ answers 404).
  const keyless: string[] = [];
  page.on("request", (r) => {
    if (/\/api\/v1\/projects\/(\?|$|\/)/.test(r.url())) keyless.push(`${r.url()} on ${page.url()}`);
  });
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey("SM");

  await page.getByRole("button", { name: "New project" }).first().click();
  const dialog = page.getByRole("dialog", { name: "New project" });
  await dialog.getByLabel("Name").fill(`Smoke ${key}`);
  await dialog.getByLabel("Key").fill(key);
  await dialog.getByRole("button", { name: /Kanban/ }).click();
  await dialog.getByRole("button", { name: "Create project" }).click();
  await expect(page).toHaveURL(new RegExp(`/projects/${key}/board`));
  await expect(page.getByRole("heading", { name: `Smoke ${key}` })).toBeVisible();

  const issue = await api(page.request).post("/issues", { projectKey: key, typeKey: "task", summary: "Smoke test issue" });

  // Quick search jumps straight to an issue key.
  const search = page.getByRole("searchbox").or(page.getByLabel("Search issues")).first();
  await search.fill(issue.key);
  await search.press("Enter");
  await expect(page).toHaveURL(new RegExp(`/browse/${issue.key}`));
  await expect(page.getByText("Smoke test issue").first()).toBeVisible();
  for (const path of [`/projects/${key}/board?issue=${issue.key}`, `/projects/${key}/settings`]) {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
  }
  expect(keyless).toEqual([]);
});

test("unknown pages and signed-out visits are handled", async ({ page }) => {
  await page.goto("/projects");
  await expect(page).toHaveURL(/\/login\?returnTo=/);
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  await page.goto("/definitely/not/a/page");
  await expect(page.getByText("Page not found")).toBeVisible();
});
