import { createHash, randomBytes } from "node:crypto";
import { expect, test, type Locator, type Page } from "@playwright/test";
import { run } from "./helpers";

// An AI assistant connects with OAuth: it registers, sends the person to /oauth/authorize, the
// person signs in, approves on the consent page and the browser returns to the assistant's
// loopback callback with a code it exchanges for tokens. The profile then lists the app.
// Set SHOTS_DIR to save screenshots of the consent page and Connected apps (light and dark).

const callback = "http://127.0.0.1:47999/callback";
const shots = process.env.SHOTS_DIR;

function pkce() {
  const verifier = randomBytes(32).toString("base64url");
  return { verifier, challenge: createHash("sha256").update(verifier).digest("base64url") };
}

function authorizeURL(clientId: string, challenge: string, state: string, extra: Record<string, string> = {}) {
  const q = new URLSearchParams({
    response_type: "code",
    client_id: clientId,
    redirect_uri: callback,
    state,
    code_challenge: challenge,
    code_challenge_method: "S256",
    scope: "mcp:read mcp:write",
    ...extra,
  });
  return `/oauth/authorize?${q}`;
}

/** Captures the browser's arrival at the assistant's loopback callback. */
async function catchCallback(page: Page) {
  const hits: URL[] = [];
  await page.route(`${callback}**`, (route) => {
    hits.push(new URL(route.request().url()));
    return route.fulfill({ contentType: "text/html", body: "<p>Back in the assistant</p>" });
  });
  return hits;
}

async function shoot(page: Page, name: string, part?: Locator) {
  if (!shots) return;
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.waitForTimeout(400); // let colour transitions finish
    const path = `${shots}/${name}-${scheme}.png`;
    await (part ? part.screenshot({ path }) : page.screenshot({ path }));
  }
  await page.emulateMedia({ colorScheme: "light" });
}

test("connect an assistant with OAuth: sign in, consent, tokens, connected apps", async ({ page }) => {
  const name = `E2E Assistant ${run}`;
  const reg = await page.request.post("/oauth/register", {
    data: { client_name: name, redirect_uris: ["http://127.0.0.1/callback"], token_endpoint_auth_method: "none" },
  });
  expect(reg.status()).toBe(201);
  const clientId = (await reg.json()).client_id as string;
  const hits = await catchCallback(page);

  // Signed out: the assistant's link leads through sign-in to the consent page.
  const p = pkce();
  await page.goto(authorizeURL(clientId, p.challenge, "e2e-state"));
  await expect(page).toHaveURL(/\/login\?returnTo=%2Foauth%2Fconsent%2F/);
  await page.getByLabel("Email").fill("e2e-oauth@example.com");
  await page.getByLabel("Display name").fill("Olive Auth");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/oauth\/consent\/[\w-]+$/);

  await expect(page.getByRole("heading", { name: `Connect ${name} to Workflow` })).toBeVisible();
  await expect(page.getByText("Olive Auth")).toBeVisible();
  await expect(page.getByText("127.0.0.1", { exact: true })).toBeVisible();
  await expect(page.getByText(/receives access through an app on this computer/)).toBeVisible();
  const readOnly = page.getByRole("radio", { name: /^Read your issues\s*Search/ });
  const readWrite = page.getByRole("radio", { name: /create, edit and move them/ });
  await expect(readWrite).toBeChecked();
  await shoot(page, "consent");

  // Choose read only and allow: the browser returns to the assistant with a code.
  await page.locator("label", { has: readOnly }).click();
  await expect(readOnly).toBeChecked();
  await page.getByRole("button", { name: "Allow access" }).click();
  await expect.poll(() => hits.length).toBe(1);
  const back = hits[0];
  expect(back.searchParams.get("state")).toBe("e2e-state");
  expect(back.searchParams.get("iss")).toBeTruthy();
  const code = back.searchParams.get("code")!;

  // The assistant exchanges the code (PKCE) and calls MCP with read tools only.
  const tok = await page.request.post("/oauth/token", {
    form: { grant_type: "authorization_code", client_id: clientId, code, redirect_uri: callback, code_verifier: p.verifier },
  });
  expect(tok.status()).toBe(200);
  const tokens = await tok.json();
  expect(tokens.scope).toBe("mcp:read");
  const list = await page.request.post("/mcp", {
    headers: { Authorization: `Bearer ${tokens.access_token}`, Accept: "application/json, text/event-stream" },
    data: { jsonrpc: "2.0", id: 1, method: "tools/list" },
  });
  expect(list.status()).toBe(200);
  const toolNames = (await list.json()).result.tools.map((t: { name: string }) => t.name);
  expect(toolNames).toContain("search_issues");
  expect(toolNames).not.toContain("create_issue");

  // Asking again for read only is remembered: /oauth/authorize answers with the code directly.
  const p2 = pkce();
  const again = await page.request.get(authorizeURL(clientId, p2.challenge, "again", { scope: "mcp:read" }), { maxRedirects: 0 });
  expect(again.status()).toBe(302);
  const direct = new URL(again.headers()["location"]);
  expect(`${direct.origin}${direct.pathname}`).toBe(callback);
  expect(direct.searchParams.get("code")).toBeTruthy();

  // Denying sends access_denied back.
  const p3 = pkce();
  await page.goto(authorizeURL(clientId, p3.challenge, "deny-me", { prompt: "consent" }));
  await page.getByRole("button", { name: "Deny" }).click();
  await expect.poll(() => hits.length).toBe(2);
  expect(hits[1].searchParams.get("error")).toBe("access_denied");
  expect(hits[1].searchParams.get("state")).toBe("deny-me");

  // Profile → Connected apps lists the assistant; disconnecting removes it.
  await page.goto("/profile");
  const section = page.locator("section", { has: page.getByRole("heading", { name: "Connected apps" }) });
  const row = section.getByRole("row", { name: new RegExp(name) });
  await expect(row).toBeVisible();
  await expect(row.getByText("Read only")).toBeVisible();
  await expect(page.getByRole("tab", { name: "Claude", exact: true })).toBeVisible();
  await shoot(page, "profile-connected-apps", section);
  await shoot(page, "profile-connect-snippets", page.locator("section", { has: page.getByRole("heading", { name: "Connect AI assistants and tools" }) }));
  if (shots) {
    await page.goto("/admin/connected-apps");
    await expect(page.getByRole("row", { name: new RegExp(name) })).toBeVisible();
    await shoot(page, "admin-connected-apps");
    await page.goto("/profile");
  }
  await row.getByRole("button", { name: `Disconnect ${name}` }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Disconnect" }).click();
  await expect(row).toHaveCount(0);
  const after = await page.request.post("/mcp", {
    headers: { Authorization: `Bearer ${tokens.access_token}`, Accept: "application/json, text/event-stream" },
    data: { jsonrpc: "2.0", id: 1, method: "tools/list" },
  });
  expect(after.status()).toBe(401);
});

test("requests that cannot be trusted stop on Workflow's own page", async ({ page }) => {
  const reg = await page.request.post("/oauth/register", { data: { client_name: "Redirect check", redirect_uris: ["https://app.example/cb"] } });
  const clientId = (await reg.json()).client_id as string;
  const p = pkce();
  const q = new URLSearchParams({ response_type: "code", client_id: clientId, redirect_uri: "https://evil.example/cb", state: "s", code_challenge: p.challenge, code_challenge_method: "S256" });
  await page.goto(`/oauth/authorize?${q}`);
  await expect(page).toHaveURL(/\/oauth\/consent\?error=invalid_redirect_uri/);
  await expect(page.getByRole("heading", { name: "This request was stopped" })).toBeVisible();
  await shoot(page, "consent-error");

  await page.goto("/oauth/consent?error=invalid_client");
  await expect(page.getByRole("heading", { name: "Workflow doesn't know this app" })).toBeVisible();
});
