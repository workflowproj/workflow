// Times the heaviest pages in a real browser against the seeded data: until the first row or
// card shows, until the page is idle, and how many DOM nodes it ends with.
import { createRequire } from "node:module";
const { chromium } = createRequire(import.meta.url)("playwright");
const BASE = process.env.BASE ?? "http://localhost:8082";
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
await page.request.post(`${BASE}/api/v1/auth/dev-login`, { headers: { "x-requested-with": "fetch" }, data: { email: "user3@scale.test", name: "User 3" } });
const pages = [
  ["board scrum", "/projects/SCRA/board", "[data-issue-key]"],
  ["board kanban", "/projects/KANA/board", "[data-issue-key]"],
  ["backlog scrum", "/projects/SCRA/backlog", "[data-issue-key]"],
  ["backlog kanban", "/projects/KANA/backlog", "[data-issue-key]"],
  ["issues", "/projects/SCRA/issues", "table tbody tr"],
  ["timeline", "/projects/SCRA/timeline", "[data-row-key]"],
  ["big picture", "/timeline", "[data-row-key]"],
  ["issue", "/browse/SCRA-10050", "h1"],
  ["reports", "/projects/SCRA/reports", "svg"],
];
for (const [name, path, sel] of pages.filter(([n]) => !process.argv[2] || n.includes(process.argv[2]))) {
  const t0 = Date.now();
  await page.goto(BASE + path);
  await page.locator(sel).first().waitFor({ timeout: 60_000 });
  const first = Date.now() - t0;
  await page.waitForLoadState("networkidle");
  const idle = Date.now() - t0;
  const nodes = await page.evaluate(() => document.getElementsByTagName("*").length);
  const rows = await page.locator(sel).count();
  // A click on the page body forces a layout; how long the main thread is blocked.
  const blocked = await page.evaluate(() => new Promise((r) => { const s = performance.now(); requestAnimationFrame(() => setTimeout(() => r(performance.now() - s), 0)); }));
  console.log(name.padEnd(16), `first ${String(first).padStart(5)} ms  idle ${String(idle).padStart(5)} ms  ${String(rows).padStart(5)} rows  ${String(nodes).padStart(6)} nodes  frame ${blocked.toFixed(0)} ms`);
}
// Opening an issue from the backlog.
await page.goto(BASE + "/projects/KANA/backlog");
await page.locator("[data-issue-key]").first().waitFor();
const t1 = Date.now();
await page.locator("[data-issue-key]").nth(5).click();
await page.getByRole("dialog").or(page.locator("aside")).first().waitFor({ timeout: 30_000 }).catch(() => {});
console.log("open from backlog".padEnd(16), `${Date.now() - t1} ms`);
await browser.close();
