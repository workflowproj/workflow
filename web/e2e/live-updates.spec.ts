import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

test("live updates: a dashboard follows its projects over one event stream", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const key = projectKey("LV");
  await a.post("/projects", { key, name: `Live ${key}`, template: "kanban" });
  const d = await a.post("/dashboards", { name: `Live ${key}`, gadgets: [{ id: "n", type: "stat", width: 1, title: "Open now", config: { projectKeys: [key], stat: "open" } }] });
  const streams: string[] = [];
  page.on("request", (r) => r.url().includes("/api/v1/events") && streams.push(r.url()));
  await page.goto(`/dashboards/${d.id}`);
  const tile = page.getByRole("heading", { name: "Open now" }).locator("xpath=ancestor::*[self::section or self::article][1]");
  await expect(tile).toContainText("0");
  await a.post("/issues", { projectKey: key, typeKey: "task", summary: "One" });
  await a.post("/issues", { projectKey: key, typeKey: "task", summary: "Two" });
  await expect(tile).toContainText("2", { timeout: 8000 });
  expect(streams.some((u) => u.includes(`projects=${key}`))).toBe(true);
});
