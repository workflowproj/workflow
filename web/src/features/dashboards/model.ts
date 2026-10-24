import type { Schemas } from "@/api/client";
import type { StatusCategory } from "@/api/types";
import { formatPoints } from "@/features/board/model";
import { formatDuration } from "@/features/reports/chart-utils";
import { quoteString, quoteValue, splitOrderBy, tokenize } from "@/features/search/jql";

export type Dashboard = Schemas["Dashboard"];
export type DashboardInput = Schemas["DashboardInput"];
export type Gadget = Schemas["Gadget"];
export type GadgetType = Schemas["GadgetType"];
export type GadgetConfig = Schemas["GadgetConfig"];
export type MetricStat = Schemas["MetricStat"];
export type BreakdownBy = Schemas["BreakdownBy"];
export type MetricsSummary = Schemas["MetricsSummary"];
type Sprint = Schemas["Sprint"];

/**
 * How a gadget picks its issues: a project's sprint (sprint gadgets), a project (velocity), or
 * projects plus JQL (everything else; no projects means every project the viewer can browse).
 */
export type GadgetScope = "sprint" | "project" | "query";

/** The settings a type uses besides its scope; the config dialog shows only these. */
export type GadgetOption = "days" | "interval" | "by" | "flowBy" | "measure" | "chart" | "stat" | "count" | "limit";

export type GadgetGroup = "Sprints" | "Flow and trends" | "Breakdowns and lists";

export type GadgetMeta = {
  type: GadgetType;
  name: string;
  description: string;
  group: GadgetGroup;
  scope: GadgetScope;
  options: GadgetOption[];
  width: 1 | 2 | 3;
  defaults: GadgetConfig;
};

export const GADGETS: GadgetMeta[] = [
  { type: "sprint_health", name: "Sprint health", description: "Days left, progress, scope change and work by status for a sprint.", group: "Sprints", scope: "sprint", options: ["measure"], width: 1, defaults: {} },
  { type: "sprint_burndown", name: "Sprint burndown", description: "Work remaining each day of a sprint against the guideline down to zero.", group: "Sprints", scope: "sprint", options: ["measure"], width: 2, defaults: {} },
  { type: "sprint_burnup", name: "Sprint burnup", description: "Work completed against the sprint's scope, which steps up when work is added.", group: "Sprints", scope: "sprint", options: ["measure"], width: 2, defaults: {} },
  { type: "velocity", name: "Velocity", description: "Committed and completed work in recent sprints, with the average.", group: "Sprints", scope: "project", options: ["count", "measure"], width: 2, defaults: { count: 7 } },
  { type: "cumulative_flow", name: "Cumulative flow", description: "Issues in each status over time; a widening band is work piling up.", group: "Flow and trends", scope: "query", options: ["days", "flowBy"], width: 2, defaults: { days: 30, flowBy: "category" } },
  { type: "created_resolved", name: "Created vs resolved", description: "Work coming in against work getting done, with the open trend.", group: "Flow and trends", scope: "query", options: ["days", "interval"], width: 2, defaults: { days: 30, interval: "day" } },
  { type: "cycle_time", name: "Cycle time", description: "How long issues took from start to done, with the median and 85th percentile.", group: "Flow and trends", scope: "query", options: ["days"], width: 2, defaults: { days: 30 } },
  { type: "stat", name: "Number", description: "One headline number: open, overdue, resolved, median cycle time and more.", group: "Breakdowns and lists", scope: "query", options: ["stat", "days"], width: 1, defaults: { stat: "open", days: 30 } },
  { type: "breakdown", name: "Breakdown", description: "Issues or points grouped by status, assignee, priority, type and more.", group: "Breakdowns and lists", scope: "query", options: ["by", "measure", "chart"], width: 1, defaults: { by: "assignee", measure: "issues", chart: "bar" } },
  { type: "issue_list", name: "Issue list", description: "The issues matching a query, as a compact table.", group: "Breakdowns and lists", scope: "query", options: ["limit"], width: 2, defaults: { limit: 10 } },
];

export function gadgetMeta(type: GadgetType): GadgetMeta {
  return GADGETS.find((g) => g.type === type) ?? GADGETS[0];
}

export const STATS: { value: MetricStat; label: string; windowed: boolean }[] = [
  { value: "open", label: "Open issues", windowed: false },
  { value: "todo", label: "To do", windowed: false },
  { value: "in_progress", label: "In progress", windowed: false },
  { value: "overdue", label: "Overdue", windowed: false },
  { value: "unassigned", label: "Unassigned", windowed: false },
  { value: "open_points", label: "Open story points", windowed: false },
  { value: "created", label: "Created", windowed: true },
  { value: "resolved", label: "Resolved", windowed: true },
  { value: "resolved_points", label: "Points resolved", windowed: true },
  { value: "median_cycle_hours", label: "Median cycle time", windowed: true },
];

export const statMeta = (s: MetricStat | undefined) => STATS.find((x) => x.value === s) ?? STATS[0];

export const BREAKDOWNS: { value: BreakdownBy; label: string }[] = [
  { value: "assignee", label: "Assignee" },
  { value: "status", label: "Status" },
  { value: "statusCategory", label: "Status category" },
  { value: "priority", label: "Priority" },
  { value: "type", label: "Issue type" },
  { value: "label", label: "Label" },
  { value: "epic", label: "Epic" },
  { value: "sprint", label: "Sprint" },
  { value: "release", label: "Release" },
  { value: "project", label: "Project" },
];

export const breakdownLabel = (by: BreakdownBy | undefined) => BREAKDOWNS.find((b) => b.value === by)?.label ?? "Assignee";

export const WINDOWS = [7, 14, 30, 60, 90, 180, 365];

// ---------------------------------------------------------------------------
// JQL

/** projectClause restricts a query to the given project keys. */
export function projectClause(keys: string[]): string {
  return keys.length === 1 ? `project = ${quoteValue(keys[0])}` : `project in (${keys.map(quoteValue).join(", ")})`;
}

const hasOr = (jql: string) => tokenize(jql).some((t) => t.kind === "word" && t.text.toLowerCase() === "or");

/**
 * scopeJql combines a gadget's projects, its JQL and extra clauses into one query, keeping the
 * JQL's ORDER BY at the end. Parts containing OR are parenthesised so AND binds as intended.
 */
export function scopeJql(projectKeys: string[] | undefined, jql: string | undefined, ...extra: (string | undefined)[]): string {
  const { where, orderBy } = splitOrderBy(jql ?? "");
  const parts = [projectKeys?.length ? projectClause(projectKeys) : "", where, ...extra].filter((p): p is string => !!p && p.trim() !== "");
  const body = parts.length > 1 ? parts.map((p) => (hasOr(p) ? `(${p})` : p)).join(" AND ") : (parts[0] ?? "");
  return [body, orderBy ? `ORDER BY ${orderBy}` : ""].filter(Boolean).join(" ");
}

/** whereOf drops ORDER BY: the metrics endpoints only filter. */
export const whereOf = (jql: string | undefined) => splitOrderBy(jql ?? "").where;

const BY_FIELD: Record<BreakdownBy, string> = {
  status: "status",
  statusCategory: "statusCategory",
  priority: "priority",
  type: "type",
  assignee: "assignee",
  label: "labels",
  epic: "epic",
  sprint: "sprint",
  release: "fixVersion",
  project: "project",
};

const CATEGORY_JQL: Record<StatusCategory, string> = { todo: '"To Do"', in_progress: '"In Progress"', done: "Done" };

/** breakdownClause is the JQL for one breakdown group (its key is an id, key or name; "" is None). */
export function breakdownClause(by: BreakdownBy, key: string): string {
  const field = BY_FIELD[by];
  if (key === "") return `${field} is EMPTY`;
  if (by === "statusCategory") return `statusCategory = ${CATEGORY_JQL[key as StatusCategory] ?? quoteString(key)}`;
  if (by === "project") return `project = ${quoteValue(key)}`;
  return `${field} = ${quoteString(key)}`;
}

/** statClause narrows a gadget's query to the issues a stat counts, for "Open in search". */
export function statClause(stat: MetricStat, days: number): string {
  switch (stat) {
    case "todo":
      return 'statusCategory = "To Do"';
    case "in_progress":
      return 'statusCategory = "In Progress"';
    case "overdue":
      return "statusCategory != Done AND due < startOfDay()";
    case "unassigned":
      return "statusCategory != Done AND assignee is EMPTY";
    case "created":
      return `created >= -${days}d`;
    case "resolved":
    case "resolved_points":
    case "median_cycle_hours":
      return `resolved >= -${days}d`;
    default:
      return "statusCategory != Done";
  }
}

// ---------------------------------------------------------------------------
// Stats

/** statValue reads a stat from the summary; undefined when there is nothing to measure. */
export function statValue(s: MetricsSummary, stat: MetricStat): number | undefined {
  const map: Record<MetricStat, number | undefined> = {
    open: s.open,
    todo: s.todo,
    in_progress: s.inProgress,
    created: s.created,
    resolved: s.resolved,
    overdue: s.overdue,
    unassigned: s.unassigned,
    open_points: s.openPoints,
    resolved_points: s.resolvedPoints,
    median_cycle_hours: s.medianCycleHours,
  };
  return map[stat];
}

const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });

export function formatStat(stat: MetricStat, v: number | undefined): string {
  if (v === undefined || v === null) return "–";
  if (stat === "median_cycle_hours") return formatDuration(v);
  if (stat === "open_points" || stat === "resolved_points") return v >= 10_000 ? compact.format(v) : formatPoints(v);
  return v >= 10_000 ? compact.format(v) : v.toLocaleString();
}

/** statCaption says what the number counts, e.g. "resolved in the last 30 days". */
export function statCaption(stat: MetricStat, days: number, s?: MetricsSummary): string {
  const win = `in the last ${days} days`;
  switch (stat) {
    case "open":
      return s ? `${s.inProgress} in progress, ${s.todo} to do` : "not done yet";
    case "todo":
      return s ? `of ${s.open} open` : "not started";
    case "in_progress":
      return s ? `of ${s.open} open` : "being worked on";
    case "overdue":
      return s ? `of ${s.open} open, past their due date` : "open, past their due date";
    case "unassigned":
      return s ? `of ${s.open} open, nobody assigned` : "open, nobody assigned";
    case "open_points":
      return s ? `on ${s.open} open issues` : "on open issues";
    case "created":
      return s ? `${win}, ${s.resolved} resolved` : win;
    case "resolved":
      return s ? `${win}, ${s.created} created` : win;
    case "resolved_points":
      return `${win}`;
    case "median_cycle_hours":
      return s ? `start to done, ${s.resolved} resolved ${win}` : `start to done, ${win}`;
  }
}

// ---------------------------------------------------------------------------
// Titles and scope

function projectsName(keys: string[] | undefined): string {
  if (!keys?.length) return "";
  return keys.length <= 3 ? keys.join(", ") : `${keys.length} projects`;
}

const withScope = (name: string, scope: string) => (scope ? `${name} · ${scope}` : name);

/** defaultTitle names a gadget from its type and settings, e.g. "Burndown · PLAT Sprint 4". */
export function defaultTitle(g: Gadget, sprintName?: string): string {
  const c = g.config;
  // Sprints are often named after the project already ("PLAT Sprint 4").
  const named = !!sprintName && !!c.projectKey && sprintName.toUpperCase().startsWith(`${c.projectKey.toUpperCase()} `);
  const sprint = named ? sprintName : [c.projectKey, sprintName].filter(Boolean).join(" ");
  switch (g.type) {
    case "sprint_burndown":
      return withScope("Burndown", sprint);
    case "sprint_burnup":
      return withScope("Burnup", sprint);
    case "sprint_health":
      return withScope("Sprint health", sprint);
    case "velocity":
      return withScope("Velocity", c.projectKey ?? "");
    case "cumulative_flow":
      return withScope("Cumulative flow", projectsName(c.projectKeys));
    case "created_resolved":
      return withScope("Created vs resolved", projectsName(c.projectKeys));
    case "cycle_time":
      return withScope("Cycle time", projectsName(c.projectKeys));
    case "breakdown":
      return withScope(`${c.measure === "points" ? "Points" : "Issues"} by ${breakdownLabel(c.by).toLowerCase()}`, projectsName(c.projectKeys));
    case "stat":
      return statMeta(c.stat).label;
    case "issue_list":
      return withScope("Issues", projectsName(c.projectKeys));
  }
}

export const gadgetTitle = (g: Gadget, sprintName?: string) => g.title?.trim() || defaultTitle(g, sprintName);

const clip = (s: string, n = 60) => (s.length > n ? `${s.slice(0, n - 1)}…` : s);

/** scopeSummary describes what a query gadget covers: projects, JQL and time window. */
export function scopeSummary(g: Gadget): string {
  const c = g.config;
  const meta = gadgetMeta(g.type);
  if (meta.scope === "project") return [c.projectKey, `last ${c.count ?? 7} completed sprints`].filter(Boolean).join(" · ");
  if (meta.scope === "sprint") return [c.projectKey, c.sprintId ? "" : "active sprint"].filter(Boolean).join(" · ");
  const where = whereOf(c.jql);
  const days = c.days ?? 30;
  let window = "";
  if (g.type === "cumulative_flow") window = `last ${days} days${c.flowBy === "status" ? ", by status" : ""}`;
  else if (g.type === "created_resolved") window = `last ${days} days, ${c.interval === "week" ? "weekly" : "daily"}`;
  else if (g.type === "cycle_time") window = `resolved in the last ${days} days`;
  else if (g.type === "stat" && statMeta(c.stat).windowed) window = `last ${days} days`;
  return [c.projectKeys?.length ? c.projectKeys.join(", ") : "All projects", where && clip(where), window].filter(Boolean).join(" · ");
}

/** searchJql is where "Open in search" goes for a query gadget (null for sprint gadgets). */
export function searchJql(g: Gadget): string | null {
  const c = g.config;
  if (gadgetMeta(g.type).scope !== "query") return null;
  const days = c.days ?? 30;
  if (g.type === "stat") return scopeJql(c.projectKeys, c.jql, statClause(c.stat ?? "open", days));
  if (g.type === "cycle_time") return scopeJql(c.projectKeys, c.jql, `resolved >= -${days}d`);
  return scopeJql(c.projectKeys, c.jql);
}

// ---------------------------------------------------------------------------
// Sprints

/** pickSprint finds the sprint a gadget shows: the chosen one, else the active, else the last completed. */
export function pickSprint(sprints: Sprint[], sprintId?: string): Sprint | undefined {
  if (sprintId) return sprints.find((s) => s.id === sprintId);
  const done = (s: Sprint) => s.completedAt ?? s.endDate ?? "";
  return sprints.find((s) => s.state === "active") ?? sprints.filter((s) => s.state === "closed").sort((a, b) => done(b).localeCompare(done(a)))[0];
}

// ---------------------------------------------------------------------------
// Editing

/** cleanConfig keeps only the settings a gadget type uses, so saved configs stay tidy. */
export function cleanConfig(type: GadgetType, c: GadgetConfig): GadgetConfig {
  const meta = gadgetMeta(type);
  const out: GadgetConfig = {};
  if (meta.scope === "query") {
    if (c.projectKeys?.length) out.projectKeys = c.projectKeys;
    if (c.jql?.trim()) out.jql = c.jql.trim();
  } else {
    if (c.projectKey) out.projectKey = c.projectKey;
    if (meta.scope === "sprint" && c.sprintId) out.sprintId = c.sprintId;
  }
  for (const o of meta.options) {
    if (o === "days" && type === "stat" && !statMeta(c.stat).windowed) continue;
    if (c[o] !== undefined && c[o] !== null) (out as Record<string, unknown>)[o] = c[o];
  }
  return out;
}

/** newGadgetId picks an id not yet used on the dashboard. */
export function newGadgetId(gadgets: Gadget[], random = Math.random): string {
  const used = new Set(gadgets.map((g) => g.id));
  for (;;) {
    const id = `g-${Math.floor(random() * 36 ** 6).toString(36).padStart(6, "0")}`;
    if (!used.has(id)) return id;
  }
}

export function newGadget(type: GadgetType, gadgets: Gadget[], seed: GadgetConfig = {}): Gadget {
  const meta = gadgetMeta(type);
  const scoped: GadgetConfig = meta.scope === "query" ? { projectKeys: seed.projectKeys } : { projectKey: seed.projectKey };
  return { id: newGadgetId(gadgets), type, width: meta.width, config: cleanConfig(type, { ...meta.defaults, ...scoped }) };
}

/** moveItem returns the list with the item at `from` moved to index `to`. */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list;
  const out = [...list];
  const [it] = out.splice(from, 1);
  out.splice(to, 0, it);
  return out;
}

export function duplicateGadget(gadgets: Gadget[], id: string): Gadget[] {
  const k = gadgets.findIndex((g) => g.id === id);
  if (k < 0) return gadgets;
  const copy: Gadget = { ...structuredClone(gadgets[k]), id: newGadgetId(gadgets) };
  return [...gadgets.slice(0, k + 1), copy, ...gadgets.slice(k + 1)];
}

/** seedScope suggests projects for a new gadget from the ones the dashboard already uses. */
/**
 * liveScope is the projects a dashboard's gadgets read, for one live event stream: every
 * browsable project as soon as one gadget isn't limited to projects.
 */
export function liveScope(gadgets: Gadget[]): string[] | "all" | undefined {
  const keys = new Set<string>();
  for (const g of gadgets) {
    if (gadgetMeta(g.type).scope !== "query") {
      if (g.config.projectKey) keys.add(g.config.projectKey);
      continue;
    }
    if (!g.config.projectKeys?.length) return "all";
    for (const k of g.config.projectKeys) keys.add(k);
  }
  return keys.size ? [...keys].sort() : undefined;
}

export function seedScope(gadgets: Gadget[]): GadgetConfig {
  const projectKey = gadgets.map((g) => g.config.projectKey ?? g.config.projectKeys?.[0]).find(Boolean);
  const keys = gadgets.find((g) => g.config.projectKeys?.length)?.config.projectKeys;
  return { projectKey, projectKeys: keys ?? (projectKey ? [projectKey] : undefined) };
}

export function toInput(d: Dashboard): DashboardInput {
  return { name: d.name, description: d.description, shared: d.shared, gadgets: d.gadgets };
}

/** stableJson serialises with sorted keys, so equal values compare equal whatever their key order. */
function stableJson(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(stableJson).join(",")}]`;
  if (v && typeof v === "object") {
    return `{${Object.keys(v)
      .filter((k) => (v as Record<string, unknown>)[k] !== undefined)
      .sort()
      .map((k) => `${JSON.stringify(k)}:${stableJson((v as Record<string, unknown>)[k])}`)
      .join(",")}}`;
  }
  return JSON.stringify(v);
}

/** sameDashboard reports whether a draft matches what is saved. */
export function sameDashboard(a: DashboardInput, b: DashboardInput): boolean {
  const norm = (d: DashboardInput) => ({ name: d.name.trim(), description: (d.description ?? "").trim(), shared: !!d.shared, gadgets: d.gadgets.map((g) => ({ ...g, title: g.title?.trim() || undefined })) });
  return stableJson(norm(a)) === stableJson(norm(b));
}
