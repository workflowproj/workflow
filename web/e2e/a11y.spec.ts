import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Accessibility: every main page (the admin pages need the first user, an organisation admin), in light and dark mode, has no serious or critical axe
// violations against WCAG 2.1 A/AA. Less severe findings are printed for follow-up.

async function audit(page: Page, name: string) {
  // Let data load and animations settle before scanning.
  await page.waitForLoadState("networkidle");
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  const blocking = results.violations.filter((v) => v.impact === "serious" || v.impact === "critical");
  for (const v of results.violations.filter((x) => !blocking.includes(x))) {
    console.log(`[${name}] ${v.impact}: ${v.id} (${v.nodes.length}) ${v.help}`);
  }
  const summary = blocking.map(
    (v) => `${v.id} (${v.impact}): ${v.help}\n${v.nodes.slice(0, 4).map((n) => `  ${n.target.join(" ")} — ${(n.failureSummary ?? "").split("\n").slice(1).join(" ").trim()}`).join("\n")}`,
  );
  expect.soft(summary, `${name}: accessibility violations`).toEqual([]);
}

test("accessibility: main pages in light and dark mode", async ({ page, browser }) => {
  test.setTimeout(180_000);
  // The sign-in page, signed out.
  const ctx = await browser.newContext();
  const anon = await ctx.newPage();
  await anon.goto("/login");
  await audit(anon, "login");
  await anon.goto("/forgot-password");
  await audit(anon, "forgot password");
  await ctx.close();

  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const a = api(page.request);
  const key = projectKey("AX");
  await a.post("/projects", { key, name: `Accessible ${key}`, template: "scrum" });
  const epic = await a.post("/issues", { projectKey: key, typeKey: "epic", summary: "Checkout" });
  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Pay by card", parentKey: epic.key, storyPoints: 3 });
  await a.post(`/issues/${story.key}/comments`, { body: "Looks **good**" });
  const dashboard = await a.post("/dashboards", {
    name: `Accessible ${key}`,
    gadgets: [
      { id: "s", type: "stat", width: 1, config: { projectKeys: [key], stat: "open" } },
      { id: "b", type: "breakdown", width: 1, config: { projectKeys: [key], by: "status" } },
      { id: "l", type: "issue_list", width: 2, config: { projectKeys: [key] } },
    ],
  });

  const pages: [string, string][] = [
    ["projects", "/projects"],
    ["board", `/projects/${key}/board`],
    ["backlog", `/projects/${key}/backlog`],
    ["timeline", `/projects/${key}/timeline`],
    ["issues", `/projects/${key}/issues`],
    ["issue", `/browse/${story.key}`],
    ["reports", `/projects/${key}/reports`],
    ["releases", `/projects/${key}/releases`],
    ["project settings", `/projects/${key}/settings`],
    ["board settings", `/projects/${key}/settings/boards`],
    ["automation", `/projects/${key}/settings/automation`],
    ["search", `/issues?jql=${encodeURIComponent(`project = ${key}`)}`],
    ["dashboards", "/dashboards"],
    ["dashboard", `/dashboards/${dashboard.id}`],
    ["big picture", "/timeline"],
    ["profile", "/profile"],
    ["timesheet", "/timesheet"],
    ["admin users", "/admin/users"],
    ["admin email", "/admin/email"],
    ["admin connected apps", "/admin/connected-apps"],
    ["admin import", "/admin/import"],
  ];
  for (const theme of ["light", "dark"] as const) {
    await page.evaluate((t) => localStorage.setItem("wfm.theme", t), theme);
    for (const [name, path] of pages) {
      await page.goto(path);
      await audit(page, `${name} (${theme})`);
    }
  }
});
