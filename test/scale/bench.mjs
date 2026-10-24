// Times the read endpoints the UI uses most against the seeded data: median and worst of N runs.
const BASE = process.env.BASE ?? "http://localhost:8082";
const RUNS = Number(process.env.RUNS ?? 5);
const only = process.argv[2] ? new RegExp(process.argv[2]) : null;
const r0 = await fetch(`${BASE}/api/v1/auth/dev-login`, { method: "POST", headers: { "content-type": "application/json", "x-requested-with": "fetch" }, body: JSON.stringify({ email: "user3@scale.test", name: "User 3" }) });
const cookie = r0.headers.getSetCookie().map((c) => c.split(";")[0]).join("; ");
async function get(path) {
  const t = performance.now();
  const r = await fetch(`${BASE}/api/v1${path}`, { headers: { cookie, "x-requested-with": "fetch" } });
  const body = await r.text();
  return { ms: performance.now() - t, status: r.status, bytes: body.length, body };
}
const json = async (p) => JSON.parse((await get(p)).body);
const q = encodeURIComponent;
const board = async (key) => (await json(`/projects/${key}/boards`))[0].id;
const sprints = await json(`/projects/SCRA/sprints`);
const active = sprints.find((s) => s.state === "active");
const closed = sprints.filter((s) => s.state === "closed").at(-1);
const scra = await board("SCRA"), kana = await board("KANA");
const P = "SCRA,SCRB,SCRC,KANA,KANB";
const cases = [
  ["projects", "/projects"],
  ["board scrum (active sprint ~1000)", `/boards/${scra}/data`],
  ["board scrum, assignee filter", `/boards/${scra}/data?jql=${q("assignee = currentUser()")}`],
  ["board kanban", `/boards/${kana}/data`],
  ["backlog scrum", `/boards/${scra}/backlog`],
  ["backlog kanban", `/boards/${kana}/backlog`],
  ["search project order updated", `/search?jql=${q("project = SCRA ORDER BY updated DESC")}`],
  ["search all, page 50", `/search?jql=&offset=2500`],
  ["search text", `/search?jql=${q('text ~ "billing"')}`],
  ["search summary contains", `/search?jql=${q('summary ~ "onboard"')}`],
  ["search my open", `/search?jql=${q("assignee = currentUser() AND resolution = Unresolved ORDER BY priority DESC")}`],
  ["search blockedIssues()", `/search?jql=${q("project = SCRA AND issue in blockedIssues()")}`],
  ["search epic", `/search?jql=${q("epic = SCRA-67")}`],
  ["search labels+priority", `/search?jql=${q("labels = perf AND priority = high ORDER BY created DESC")}`],
  ["search status changed", `/search?jql=${q("project in (SCRA, SCRB) AND status = Done AND resolved >= -14d")}`],
  ["issue", `/issues/SCRA-10050`],
  ["issue history", `/issues/SCRA-10050/history`],
  ["issue comments", `/issues/SCRA-10050/comments`],
  ["issue metrics", `/issues/SCRA-10050/metrics`],
  ["epic issue (children)", `/issues/SCRA-67`],
  ["timeline project", `/timeline?projects=SCRA`],
  ["timeline all projects", `/timeline`],
  ["metrics summary 5 projects", `/metrics/summary?projects=${P}&days=30`],
  ["metrics flow 90d", `/metrics/flow?projects=${P}&days=90`],
  ["metrics flow by status 365d", `/metrics/flow?projects=SCRA&days=365&by=status`],
  ["metrics created-resolved 90d", `/metrics/created-resolved?projects=${P}&days=90`],
  ["metrics breakdown status", `/metrics/breakdown?projects=${P}&by=status`],
  ["metrics breakdown epic", `/metrics/breakdown?projects=${P}&by=epic`],
  ["metrics breakdown assignee+jql", `/metrics/breakdown?by=assignee&jql=${q("resolution = Unresolved")}`],
  ["metrics cycle-time 90d", `/metrics/cycle-time?projects=${P}&days=90`],
  ["velocity", `/projects/SCRA/velocity?count=20`],
  ["report cycle-time", `/projects/SCRA/reports/cycle-time?days=90`],
  ["sprint report active (burndown)", `/sprints/${active.id}/report`],
  ["sprint report closed", `/sprints/${closed.id}/report`],
  ["sprints list", `/projects/SCRA/sprints`],
  ["labels", `/projects/SCRA/labels`],
  ["users", `/users?query=user`],
  ["search fields", `/search/fields`],
  ["notifications", `/notifications`],
];
const rows = [];
for (const [name, path] of cases) {
  if (only && !only.test(name)) continue;
  const first = await get(path);
  if (first.status !== 200) { rows.push([name, `HTTP ${first.status} ${first.body.slice(0, 120)}`]); continue; }
  const times = [first.ms];
  for (let i = 1; i < RUNS; i++) times.push((await get(path)).ms);
  times.sort((a, b) => a - b);
  rows.push([name, `${times[Math.floor(times.length / 2)].toFixed(0).padStart(6)} ms  max ${times.at(-1).toFixed(0).padStart(6)} ms  ${(first.bytes / 1024).toFixed(0).padStart(6)} KB`]);
}
for (const [n, v] of rows) console.log(n.padEnd(36), v);
