import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Risks and decisions: turning the section on, the risk register with its matrix, a risk's
// panel and mitigating work, and the decision log with superseding and the ADR export. None of
// it reaches the board.

test("risks: turn the section on, assess a risk, mitigate it and filter the matrix", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey("RK");
  const a = api(page.request);
  // A service desk starts without the section; its settings turn it on.
  await a.post("/projects", { key, name: `Risky ${key}`, template: "service" });
  await page.goto(`/projects/${key}/settings`);
  await expect(page.getByRole("link", { name: "Risks", exact: true })).toHaveCount(0);
  const toggle = page.getByRole("switch", { name: "Risks and decisions" });
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", "true");
  await expect(page.getByRole("link", { name: "Risks", exact: true })).toBeVisible();

  // New risk opens the create dialog preset to Risk, with its own fields.
  await page.getByRole("link", { name: "Risks", exact: true }).click();
  await expect(page.getByText("No risks yet")).toBeVisible();
  await page.getByRole("button", { name: "New risk" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Create issue" });
  await expect(dialog.getByLabel("Issue type")).toHaveValue("risk");
  await expect(dialog.getByLabel("Sprint")).toHaveCount(0);
  await expect(dialog.getByLabel("Story points")).toHaveCount(0);
  await dialog.getByLabel("Summary").fill("Ticket volume doubles after the launch");
  await dialog.getByLabel("Likelihood").selectOption("4");
  await dialog.getByLabel("Impact").selectOption("4");
  await expect(dialog.getByText("16 High")).toBeVisible();
  await dialog.getByLabel("Response").selectOption("Reduce");
  await dialog.getByLabel("Mitigation plan").fill("Hire two temporary agents.");
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toBeHidden();

  // The register counts it, the matrix places it, and its cell filters the table.
  await expect(page.getByText("Open risks", { exact: true }).locator("..")).toContainText("1");
  await expect(page.getByText("High and critical", { exact: true }).locator("..")).toContainText("1");
  const cell = page.getByRole("button", { name: /Likelihood 4 Likely, impact 4 Major: score 16, high; 1 open risk/ });
  await expect(cell).toBeVisible();
  const other = await a.post("/issues", { projectKey: key, typeKey: "risk", summary: "Help centre goes down", fields: { risk_likelihood: 1, risk_impact: 5 } });
  await page.reload();
  await expect(page.getByRole("cell", { name: other.summary })).toBeVisible();
  await cell.click();
  await expect(page).toHaveURL(/cell=4-4/);
  await expect(page.getByRole("cell", { name: other.summary })).toHaveCount(0);
  await expect(page.getByRole("cell", { name: /Ticket volume doubles/ })).toBeVisible();

  // The risk's panel; mitigating work created from it is linked and counted.
  await page.getByRole("link", { name: "Ticket volume doubles after the launch" }).click();
  const drawer = page.getByRole("dialog", { name: /^Issue / });
  await expect(drawer.getByRole("region", { name: "Risk", exact: true })).toContainText("16 High");
  await drawer.getByRole("button", { name: "Create mitigating issue" }).click();
  const create = page.getByRole("dialog", { name: "Create issue" });
  await create.getByLabel("Issue type").selectOption("change");
  await create.getByLabel("Summary").fill("Hire temporary agents");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(create).toBeHidden();
  await expect(drawer.getByText("0 of 1 done")).toBeVisible();
  await expect(drawer.getByRole("link", { name: "Hire temporary agents" })).toBeVisible();

  // Lowering the impact rescores it.
  await drawer.getByRole("button", { name: "Impact", exact: true }).click();
  await page.getByRole("menuitemradio", { name: "2 · Minor" }).click();
  await expect(drawer.getByRole("region", { name: "Risk", exact: true })).toContainText("8 Medium");
});

test("decisions: record, accept, supersede and export, never on the board", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey("DC");
  const a = api(page.request);
  await a.post("/projects", { key, name: `Decided ${key}`, template: "kanban" });
  const story = await a.post("/issues", { projectKey: key, typeKey: "story", summary: "Move sessions out of memory" });

  await page.goto(`/projects/${key}/decisions`);
  await page.getByRole("button", { name: "New decision" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Create issue" });
  await expect(dialog.getByLabel("Issue type")).toHaveValue("decision");
  await dialog.getByLabel("Summary").fill("Store sessions in PostgreSQL");
  await dialog.getByLabel("Context").fill("Sessions are lost on restart.");
  await dialog.getByLabel("Options considered").fill("Redis or PostgreSQL.");
  await dialog.getByRole("textbox", { name: "Decision", exact: true }).fill("PostgreSQL: one fewer service.");
  await dialog.getByLabel("Consequences").fill("More database load.");
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.getByRole("row", { name: /Store sessions in PostgreSQL/ });
  await expect(row).toContainText("Proposed");
  const decision = (await row.getByRole("link").first().textContent())!.trim();
  await a.post(`/issues/${story.key}/links`, { type: "implements", targetKey: decision });

  // Accept it from the issue view: it is dated and the work implementing it shows.
  await page.goto(`/browse/${decision}`);
  await page.getByRole("button", { name: /^Status: Proposed/ }).click();
  await page.getByRole("menuitem", { name: /^Accept/ }).click();
  const panel = page.getByRole("region", { name: "Decision", exact: true });
  await expect(panel).toContainText("0 of 1 done");
  await expect(panel.getByRole("button", { name: "Decided on", exact: true })).not.toHaveText("None");

  // Export as Markdown downloads the record.
  const download = page.waitForEvent("download");
  await panel.getByRole("button", { name: "Export as Markdown" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe(`${decision}-store-sessions-in-postgresql.md`);
  const md = await readFile((await file.path())!, "utf8");
  expect(md).toContain(`# ${decision}: Store sessions in PostgreSQL`);
  expect(md).toContain("- Status: Accepted");
  expect(md).toContain("## Decision\n\nPostgreSQL: one fewer service.");

  // Supersede it with a new decision created on the spot.
  await panel.getByRole("button", { name: "Supersede", exact: true }).click();
  await page.getByRole("dialog", { name: `Supersede ${decision}` }).getByRole("button", { name: "New decision" }).click();
  const next = page.getByRole("dialog", { name: "Create issue" });
  await next.getByLabel("Summary").fill("Store sessions in Redis");
  await next.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("button", { name: /^Status: Superseded/ })).toBeVisible();
  await expect(panel).toContainText("Store sessions in Redis");

  await page.goto(`/projects/${key}/decisions`);
  await expect(page.getByRole("row", { name: /Store sessions in PostgreSQL/ })).toContainText("Store sessions in Redis");
  await page.getByRole("tab", { name: /Superseded/ }).click();
  // Only the superseded one: the Redis decision is still proposed.
  await expect(page.getByRole("row", { name: /Proposed/ })).toHaveCount(0);
  await expect(page.getByRole("row", { name: /Store sessions in PostgreSQL/ })).toBeVisible();

  // Only the story is on the board.
  await page.goto(`/projects/${key}/board`);
  await expect(page.getByText(story.summary)).toBeVisible();
  await expect(page.getByText("Store sessions in PostgreSQL")).toHaveCount(0);
});
