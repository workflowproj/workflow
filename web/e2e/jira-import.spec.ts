import { expect, test } from "@playwright/test";
import { signIn } from "./helpers";

/*
 * The Import from Jira wizard against the fake Jira Cloud of the Go tests:
 *
 *   go run ./internal/jiraimport/jiratest/fakejira -addr 127.0.0.1:8599
 *   E2E_FAKE_JIRA=http://127.0.0.1:8599 npx playwright test jira-import
 *
 * The server must be allowed to reach it (WFM_OUTBOUND_ALLOWED_HOSTS=127.0.0.1, or
 * WFM_WEBHOOK_ALLOW_PRIVATE=true). Skipped when E2E_FAKE_JIRA is not set (e.g. in the k3d CI
 * run, where the cluster cannot reach a fake on the runner).
 */
const fake = process.env.E2E_FAKE_JIRA;

test.skip(!fake, "set E2E_FAKE_JIRA to the URL of a running fakejira");

test("connect, choose a project, map, dry-run and import from Jira", async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  await page.goto("/admin/import");
  await expect(page.getByRole("heading", { name: "Import from Jira" })).toBeVisible();
  const newImport = page.getByRole("button", { name: "New import" });
  await expect(page.getByLabel("Site URL").or(newImport)).toBeVisible();
  if (await newImport.isVisible()) await newImport.click();

  // Connect: test, then save.
  await page.getByRole("button", { name: /Jira Cloud/ }).click();
  await page.getByLabel("Site URL").fill(fake!);
  await page.getByLabel("Account email").fill("importer@example.com");
  await page.getByLabel("API token").fill("cloud-api-token");
  await page.getByRole("button", { name: "Test connection" }).click();
  await expect(page.getByText("Connected to Fake Jira")).toBeVisible();
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/import\/[0-9a-f-]{36}$/);

  // Projects.
  await page.getByLabel("Import PLAT").check();
  await expect(page.getByLabel("Template for PLAT")).toBeVisible();
  await page.getByRole("button", { name: "Save and analyse" }).click();

  // Mapping, pre-filled from the analysis.
  await expect(page.getByRole("heading", { name: "Statuses · PLAT" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByLabel("Status for In Review")).toHaveValue("In Review");
  await expect(page.getByLabel("Type for Spike")).toHaveValue("task");
  await expect(page.getByLabel("Priority for Blocker")).toHaveValue("highest");
  // First run: Cy (email hidden) would get an unclaimable placeholder; later runs reuse it.
  await expect(page.getByText(/Placeholder nobody can claim \(no email\)|Placeholder from an earlier import/).first()).toBeVisible();
  // Cy's email is hidden in Jira: give the placeholder one so Cy can claim it.
  await page.getByLabel("Mapping for Cy Hidden").selectOption("placeholder");
  await page.getByLabel("Placeholder email for Cy Hidden").fill("cy@example.com");
  await page.getByRole("button", { name: "Continue to dry run" }).click();

  // Dry run.
  await expect(page.getByRole("heading", { name: "What will be imported" })).toBeVisible();
  await expect(page.getByText("Ready to import")).toBeVisible();
  await page.getByRole("button", { name: "Start import" }).click();

  // Import with progress, then the report.
  await expect(page.getByText("Imported", { exact: true }).first()).toBeVisible({ timeout: 60_000 });
  await expect(page.getByRole("heading", { name: "Report" })).toBeVisible();
  await expect(page.getByText(/Issues (created|unchanged)/).first()).toBeVisible();
  await page.getByRole("link", { name: "PLAT", exact: true }).click();
  await expect(page).toHaveURL(/\/projects\/PLAT\/board/);
  await page.goto("/browse/PLAT-2");
  await expect(page.getByText("Login page").first()).toBeVisible();
});
