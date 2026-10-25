// Creates users, projects, sprints, releases and one issue per type through the API.
const BASE = process.env.BASE ?? "http://localhost:8082";
async function login(email, name) {
  const r = await fetch(`${BASE}/api/v1/auth/dev-login`, { method: "POST", headers: { "content-type": "application/json", "x-requested-with": "fetch" }, body: JSON.stringify({ email, name }), redirect: "manual" });
  const cookie = r.headers.getSetCookie().map((c) => c.split(";")[0]).join("; ");
  if (!cookie) throw new Error(`login ${email}: ${r.status} ${await r.text()}`);
  return cookie;
}
async function call(cookie, method, path, body) {
  const r = await fetch(`${BASE}/api/v1${path}`, { method, headers: { cookie, "content-type": "application/json", "x-requested-with": "fetch" }, body: body && JSON.stringify(body) });
  const t = await r.text();
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${t}`);
  return t ? JSON.parse(t) : null;
}
const admin = await login("admin@scale.test", "Scale Admin");
for (let i = 1; i < 30; i++) await login(`user${i}@scale.test`, `User ${i}`);
const projects = [["SCRA", "scrum"], ["SCRB", "scrum"], ["SCRC", "scrum"], ["KANA", "kanban"], ["KANB", "kanban"]];
for (const [key, template] of projects) {
  await call(admin, "POST", "/projects", { key, name: `Scale ${key}`, template });
  if (template === "scrum") for (let s = 0; s < 26; s++) await call(admin, "POST", `/projects/${key}/sprints`, { name: `${key} Sprint ${s + 1}` }).catch((e) => console.log(e.message));
  for (let v = 0; v < 10; v++) await call(admin, "POST", `/projects/${key}/releases`, { name: `${key} 1.${v}` });
  const types = await call(admin, "GET", `/projects/${key}/issue-types`);
  for (const t of types) await call(admin, "POST", "/issues", { projectKey: key, typeKey: t.key, summary: `Seed ${t.key}`, ...(t.level === -1 ? { parentKey: `${key}-1` } : {}) }).catch((e) => console.log(e.message));
}
console.log(admin);
