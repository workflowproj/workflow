import { expect, type APIRequestContext, type Page } from "@playwright/test";

/** A run-unique suffix so tests can share one database across runs. */
export const run = Date.now().toString(36).slice(-5).toUpperCase();

export async function signIn(page: Page, email: string, name: string) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Display name").fill(name);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/projects/);
}

/** api calls the REST API with the page's session (cookie) and the CSRF header. */
export function api(request: APIRequestContext) {
  const headers = { "X-Requested-With": "e2e" };
  const call = async (method: string, path: string, data?: unknown) => {
    const res = await request.fetch(`/api/v1${path}`, { method, data, headers });
    if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()} ${await res.text()}`);
    return res.status() === 204 ? null : res.json();
  };
  return {
    get: (p: string) => call("GET", p),
    post: (p: string, d?: unknown) => call("POST", p, d ?? {}),
    patch: (p: string, d: unknown) => call("PATCH", p, d),
  };
}

/** projectKey makes a valid, unique project key from a prefix. */
export function projectKey(prefix: string) {
  return (prefix + run).replace(/[^A-Z0-9]/g, "").slice(0, 10);
}
