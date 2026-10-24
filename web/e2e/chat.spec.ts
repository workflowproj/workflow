import { expect, test } from "@playwright/test";
import { api, projectKey, signIn } from "./helpers";

// Chat: add a Slack channel, edit it without re-entering the write-only webhook URL, post to it
// from an automation rule template and remove it. Nothing is sent: no event or test message fires.
test("chat channels: add, edit, use in a rule, remove", async ({ page }) => {
  await signIn(page, "e2e-admin@example.com", "E2E Admin");
  const key = projectKey("CH");
  const a = api(page.request);
  await a.post("/projects", { key, name: `Chat ${key}`, template: "scrum" });

  // Without channels the chat template is not offered.
  await page.goto(`/projects/${key}/settings/automation`);
  await expect(page.getByRole("button", { name: "Use template: Close the epic when its last story is done" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Use template: Announce finished work in chat" })).toHaveCount(0);

  await page.goto(`/projects/${key}/settings/chat`);
  await expect(page.getByText("No chat channels yet")).toBeVisible();
  await page.getByRole("button", { name: "Add channel" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Add a chat channel" });
  await dialog.getByLabel(/^Name/).fill("#e2e-team");
  await dialog.getByLabel(/^Webhook URL/).fill("https://hooks.slack.com/services/T000/B000/abcd1234");
  await dialog.getByLabel("Pipeline failed").check();
  await dialog.getByRole("button", { name: "Add channel" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText("hooks.slack.com/…1234")).toBeVisible();
  await expect(page.getByText("Pipeline failed")).toBeVisible();

  await page.getByRole("button", { name: "Edit #e2e-team" }).click();
  const edit = page.getByRole("dialog", { name: "Edit #e2e-team" });
  await expect(edit.getByLabel(/^Webhook URL/)).toHaveValue("");
  await edit.getByLabel(/^Name/).fill("#e2e-squad");
  await edit.getByRole("button", { name: "Save" }).click();
  await expect(edit).toBeHidden();
  const [channel] = await a.get(`/projects/${key}/chat-channels`);
  expect(channel).toMatchObject({ name: "#e2e-squad", urlHint: "hooks.slack.com/…1234", active: true });

  await page.goto(`/projects/${key}/settings/automation`);
  await page.getByRole("button", { name: "Use template: Announce finished work in chat" }).click();
  const rule = page.getByRole("dialog", { name: "New rule from a template" });
  await expect(rule.getByLabel("Action 1: channel")).toHaveValue(channel.id);
  await expect(rule.getByLabel("Action 1: message")).toHaveValue("🎉 {{issue.key}} {{issue.summary}} is done");
  await rule.getByRole("button", { name: "Create rule" }).click();
  await expect(rule).toBeHidden();
  await expect(page.getByText("post to “#e2e-squad”")).toBeVisible();

  await page.goto(`/projects/${key}/settings/chat`);
  await page.getByRole("button", { name: "Remove #e2e-squad" }).click();
  await page.getByRole("button", { name: "Remove channel" }).click();
  await expect(page.getByText("No chat channels yet")).toBeVisible();
  await page.goto(`/projects/${key}/settings/automation`);
  await expect(page.getByText("post to a removed channel")).toBeVisible();
});
