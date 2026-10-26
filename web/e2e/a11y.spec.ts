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
  test.setTimeout(360_000);
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
  // A release with published notes, overdue so its readiness has warnings to show.
  const yesterday = new Date(Date.now() - 86_400_000).toISOString().slice(0, 10);
  const release = await a.post(`/projects/${key}/releases`, { name: "1.0", description: "The first one", releaseDate: yesterday, notes: "## Features\n\n- Pay by card", notesPublished: true });
  const epic = await a.post("/issues", { projectKey: key, typeKey: "epic", summary: "Checkout" });
  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Pay by card", parentKey: epic.key, storyPoints: 3, releaseIds: [release.id] });
  await a.post(`/issues/${story.key}/comments`, { body: "Looks **good**" });
  // The risk register and decision log (Scrum projects keep them).
  const risk = await a.post("/issues", { projectKey: key, typeKey: "risk", summary: "Card scheme fees rise", fields: { risk_likelihood: 4, risk_impact: 5, risk_response: "Reduce", risk_mitigation: "Negotiate a cap." } });
  await a.post(`/issues/${story.key}/links`, { type: "mitigates", targetKey: risk.key });
  await a.post("/issues", { projectKey: key, typeKey: "risk", summary: "Office move delays", fields: { risk_likelihood: 1, risk_impact: 2 } });
  const decision = await a.post("/issues", { projectKey: key, typeKey: "decision", summary: "Take cards through one provider", fields: { decision_context: "Two providers double the work.", decision_outcome: "One provider." } });
  await a.post(`/issues/${decision.key}/transitions`, { transitionId: "accept" });
  const dashboard = await a.post("/dashboards", {
    name: `Accessible ${key}`,
    gadgets: [
      { id: "s", type: "stat", width: 1, config: { projectKeys: [key], stat: "open" } },
      { id: "b", type: "breakdown", width: 1, config: { projectKeys: [key], by: "status" } },
      { id: "l", type: "issue_list", width: 2, config: { projectKeys: [key] } },
    ],
  });
  // A service desk with a request (a reply to the customer and an internal note) and one waiting for the admin's approval.
  const sd = projectKey("AXSD");
  await a.post("/projects", { key: sd, name: `Help ${sd}`, template: "service" });
  const me = await a.get("/me");
  const types: { id: string; name: string; description: string; icon: string; issueType: { key: string }; group: string; position: number; fields: { field: string; label: string; required: boolean }[] }[] =
    await a.get(`/projects/${sd}/request-types`);
  const help = types.find((t) => t.name === "Get IT help")!;
  const hardware = types.find((t) => t.name === "Request new hardware")!;
  const request = await a.post(`/portal/projects/${sd}/requests`, { requestTypeId: help.id, values: { summary: "The printer is jammed", description: "Paper stuck in tray 2." } });
  await a.post(`/issues/${request.key}/comments`, { body: "On my way with a screwdriver.", visibility: "public" });
  await a.post(`/issues/${request.key}/comments`, { body: "Tray 2 roller is worn.", visibility: "internal" });
  const put = await page.request.put(`/api/v1/request-types/${hardware.id}`, {
    headers: { "X-Requested-With": "e2e" },
    data: {
      name: hardware.name,
      description: hardware.description,
      icon: hardware.icon,
      issueTypeKey: hardware.issueType.key,
      group: hardware.group,
      position: hardware.position,
      fields: hardware.fields.map((f) => ({ field: f.field, label: f.label, required: f.required })),
      approval: { mode: "any", approverIds: [me.id], approvedStatus: "Waiting for support", declinedStatus: "Declined" },
    },
  });
  expect(put.ok(), await put.text()).toBeTruthy();
  const pending = await a.post(`/portal/projects/${sd}/requests`, { requestTypeId: hardware.id, values: { summary: "A standing desk" } });
  await a.patch(`/projects/${sd}/service-desk`, { selfSignup: true, releaseNotesProjectKeys: [key] });

  const pages: [string, string][] = [
    ["projects", "/projects"],
    ["board", `/projects/${key}/board`],
    ["backlog", `/projects/${key}/backlog`],
    ["timeline", `/projects/${key}/timeline`],
    ["issues", `/projects/${key}/issues`],
    ["issue", `/browse/${story.key}`],
    ["reports", `/projects/${key}/reports`],
    ["releases", `/projects/${key}/releases`],
    ["release", `/projects/${key}/releases/${release.id}`],
    ["risks", `/projects/${key}/risks`],
    ["risks filtered by a cell", `/projects/${key}/risks?cell=4-5`],
    ["decisions", `/projects/${key}/decisions`],
    ["risk", `/browse/${risk.key}`],
    ["decision", `/browse/${decision.key}`],
    ["issue types settings", `/projects/${key}/settings/issue-types`],
    ["project settings", `/projects/${key}/settings`],
    ["release settings", `/projects/${key}/settings/releases`],
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
    ["queues", `/projects/${sd}/queues`],
    ["queue with an open request", `/projects/${sd}/queues?issue=${request.key}`],
    ["customers", `/projects/${sd}/customers`],
    ["service request", `/browse/${request.key}`],
    ["help centre settings", `/projects/${sd}/settings/portal`],
    ["request types settings", `/projects/${sd}/settings/request-types`],
    ["queues settings", `/projects/${sd}/settings/queues`],
    ["SLA settings", `/projects/${sd}/settings/slas`],
    ["portal home", "/portal"],
    ["portal desk", `/portal/${sd}`],
    ["portal what's new", `/portal/${sd}/whats-new`],
    ["portal form", `/portal/${sd}/new/${help.id}`],
    ["portal requests", "/portal/requests"],
    ["portal request", `/portal/requests/${request.key}?created=true`],
    ["portal approval", `/portal/requests/${pending.key}`],
    ["portal approvals", "/portal/approvals"],
    ["portal profile", "/portal/profile"],
  ];
  for (const theme of ["light", "dark"] as const) {
    await page.evaluate((t) => localStorage.setItem("wfm.theme", t), theme);
    for (const [name, path] of pages) {
      await page.goto(path);
      await audit(page, `${name} (${theme})`);
    }
    // The create dialog preset to a risk.
    await page.goto(`/projects/${key}/risks`);
    await page.getByRole("button", { name: "New risk" }).click();
    await expect(page.getByRole("dialog", { name: "Create issue" })).toBeVisible();
    await audit(page, `new risk dialog (${theme})`);
    // The request type form builder, open.
    await page.goto(`/projects/${sd}/settings/request-types`);
    await page.getByRole("button", { name: `Edit ${hardware.name}` }).click();
    await expect(page.getByRole("dialog", { name: `Edit ${hardware.name}` })).toBeVisible();
    await audit(page, `request type form builder (${theme})`);
    // The Release dialog, with its readiness warnings.
    await page.goto(`/projects/${key}/releases/${release.id}`);
    await page.getByRole("button", { name: "Release", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "Release 1.0" }).getByText(/not done/)).toBeVisible();
    await audit(page, `release dialog (${theme})`);
  }
  // The help centre's sign-up page, signed out.
  const anonDesk = await browser.newContext();
  const visitor = await anonDesk.newPage();
  for (const theme of ["light", "dark"] as const) {
    await visitor.goto("/login");
    await visitor.evaluate((t) => localStorage.setItem("wfm.theme", t), theme);
    await visitor.goto(`/portal/signup/${sd}`);
    await expect(visitor.getByRole("heading", { name: /Get help from/ })).toBeVisible();
    await audit(visitor, `portal sign-up (${theme})`);
  }
  await anonDesk.close();
});
