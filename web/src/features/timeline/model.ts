import type { Schemas } from "@/api/client";
import type { Issue } from "@/api/types";

/*
 * Pure timeline logic: calendar days as integers, the dates each issue is drawn with (its own,
 * or derived from its sprint, its children or a single date), the rows of the tree, dependency
 * conflicts and the header counts.
 */

export type TimelineData = Schemas["Timeline"];
export type TimelineLink = Schemas["TimelineLink"];
type ProjectRef = Schemas["ProjectRef"];
type IssueRef = Schemas["IssueRef"];

// ---------------------------------------------------------------------------
// Days: integers counted from 1970-01-01 (UTC), so date arithmetic never meets DST.

const MS = 86_400_000;

export function toDay(iso: string): number {
  const [y, m, d] = iso.slice(0, 10).split("-").map(Number);
  return Math.round(Date.UTC(y, m - 1, d) / MS);
}

export function fromDay(day: number): string {
  return new Date(day * MS).toISOString().slice(0, 10);
}

export function todayDay(now = new Date()): number {
  return Math.round(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) / MS);
}

/** weekday is 0 for Sunday … 6 for Saturday. */
export const weekday = (day: number) => (((day + 4) % 7) + 7) % 7;

const dayFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
const dayYearFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" });

/** formatDay renders "Oct 24" (with the year when it isn't `thisYear`). */
export function formatDay(day: number, thisYear = new Date().getFullYear()): string {
  const d = new Date(day * MS);
  return (d.getUTCFullYear() === thisYear ? dayFmt : dayYearFmt).format(d);
}

export function formatSpan(start: number, end: number): string {
  return start === end ? formatDay(start) : `${formatDay(start)} – ${formatDay(end)}`;
}

// ---------------------------------------------------------------------------
// Schedules

export type ScheduleSource = "own" | "sprint" | "children" | "due" | "start";

/** The days a bar covers (inclusive) and why: "own" dates, or derived ones drawn differently. */
export type Schedule = { start: number; end: number; source: ScheduleSource; note?: string };

/** A lone date is drawn as a short bar of this many days ending (or starting) on it. */
export const SHORT_BAR_DAYS = 5;

export function isDone(i: { status: { category: string } }) {
  return i.status.category === "done";
}

/**
 * schedules decides how every issue is drawn: both dates → its own; an epic without dates →
 * the span of its children; a lone date → a short bar on it; an issue in a sprint → the
 * sprint's dates. Issues with none of these get no entry.
 */
export function schedules(data: Pick<TimelineData, "issues" | "sprints">): Map<string, Schedule> {
  const out = new Map<string, Schedule>();
  const sprints = new Map(data.sprints.map((s) => [s.sprint.id, s.sprint]));
  const children = new Map<string, Issue[]>();
  for (const i of data.issues) {
    if (!i.parent) continue;
    const list = children.get(i.parent.key) ?? [];
    list.push(i);
    children.set(i.parent.key, list);
  }
  const own = (i: Issue): Schedule | undefined => {
    if (i.startDate && i.dueDate) {
      const a = toDay(i.startDate);
      const b = toDay(i.dueDate);
      return { start: Math.min(a, b), end: Math.max(a, b), source: "own" };
    }
    if (i.dueDate) {
      const d = toDay(i.dueDate);
      return { start: d - SHORT_BAR_DAYS + 1, end: d, source: "due", note: `Only a due date is set (${formatDay(d)}), so this short bar ends on it.` };
    }
    if (i.startDate) {
      const d = toDay(i.startDate);
      return { start: d, end: d + SHORT_BAR_DAYS - 1, source: "start", note: `Only a start date is set (${formatDay(d)}), so this short bar starts on it.` };
    }
    return undefined;
  };
  // Standard issues first: epics derive from them.
  for (const i of data.issues) {
    if (i.type.level >= 1) continue;
    let s = own(i);
    const sp = i.sprintId ? sprints.get(i.sprintId) : undefined;
    if (!s && sp?.startDate && sp.endDate) {
      const a = toDay(sp.startDate);
      const b = toDay(sp.endDate);
      s = { start: a, end: Math.max(a, b), source: "sprint", note: `No dates of its own: drawn with its sprint, ${sp.name} (${formatSpan(a, b)}).` };
    }
    if (s) out.set(i.key, s);
  }
  for (const i of data.issues) {
    if (i.type.level < 1) continue;
    let s: Schedule | undefined = i.startDate && i.dueDate ? own(i) : undefined;
    if (!s) {
      const spans = (children.get(i.key) ?? []).map((c) => out.get(c.key)).filter((x): x is Schedule => !!x);
      if (spans.length) {
        const a = Math.min(...spans.map((x) => x.start));
        const b = Math.max(...spans.map((x) => x.end));
        s = { start: a, end: b, source: "children", note: `No dates of its own: spans its ${spans.length} scheduled issue${spans.length === 1 ? "" : "s"}.` };
      }
    }
    s ??= own(i);
    if (s) out.set(i.key, s);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Rows

export type Row =
  | { kind: "project"; id: string; project: ProjectRef; count: number }
  | { kind: "group"; id: string; projectKey: string; label: string; parent?: IssueRef; count: number; depth: number }
  | { kind: "issue"; id: string; projectKey: string; issue: Issue; depth: number; childCount: number };

/**
 * buildRows flattens projects → epics → issues into the visible rows. Issues without an epic
 * go under "No epic"; children whose epic was filtered out are grouped under its name.
 * `include` filters issues (an epic stays when any child matches); `collapsed` holds row ids;
 * `hideEmpty` drops projects left without issues (when filtering).
 */
export function buildRows(
  data: Pick<TimelineData, "projects" | "issues">,
  opts: { multi: boolean; collapsed: Set<string>; include?: (i: Issue) => boolean; hideEmpty?: boolean },
): Row[] {
  const include = opts.include ?? (() => true);
  const rows: Row[] = [];
  const byProject = new Map<string, Issue[]>();
  for (const i of data.issues) {
    const list = byProject.get(i.projectKey) ?? [];
    list.push(i);
    byProject.set(i.projectKey, list);
  }
  for (const project of data.projects) {
    const issues = byProject.get(project.key) ?? [];
    const base = opts.multi ? 1 : 0;
    const epics = issues.filter((i) => i.type.level >= 1);
    const epicKeys = new Set(epics.map((e) => e.key));
    const children = new Map<string, Issue[]>();
    const orphans = new Map<string, { parent: IssueRef; issues: Issue[] }>();
    const loose: Issue[] = [];
    for (const i of issues) {
      if (i.type.level >= 1) continue;
      if (!i.parent) loose.push(i);
      else if (epicKeys.has(i.parent.key)) children.set(i.parent.key, [...(children.get(i.parent.key) ?? []), i]);
      else {
        const g = orphans.get(i.parent.key) ?? { parent: i.parent, issues: [] };
        g.issues.push(i);
        orphans.set(i.parent.key, g);
      }
    }
    const own: Row[] = [];
    let count = 0;
    for (const epic of epics) {
      const kids = (children.get(epic.key) ?? []).filter(include);
      if (!include(epic) && kids.length === 0) continue;
      count += 1 + kids.length;
      own.push({ kind: "issue", id: epic.key, projectKey: project.key, issue: epic, depth: base, childCount: kids.length });
      if (!opts.collapsed.has(epic.key)) for (const k of kids) own.push({ kind: "issue", id: k.key, projectKey: project.key, issue: k, depth: base + 1, childCount: 0 });
    }
    const group = (id: string, label: string, list: Issue[], parent?: IssueRef) => {
      const kids = list.filter(include);
      if (kids.length === 0) return;
      count += kids.length;
      own.push({ kind: "group", id, projectKey: project.key, label, parent, count: kids.length, depth: base });
      if (!opts.collapsed.has(id)) for (const k of kids) own.push({ kind: "issue", id: k.key, projectKey: project.key, issue: k, depth: base + 1, childCount: 0 });
    };
    for (const [key, g] of orphans) group(`${project.key}:parent:${key}`, g.parent.summary, g.issues, g.parent);
    group(`${project.key}:none`, "No epic", loose);
    if (opts.multi) {
      if (count === 0 && opts.hideEmpty) continue;
      rows.push({ kind: "project", id: `project:${project.key}`, project, count });
      if (!opts.collapsed.has(`project:${project.key}`)) rows.push(...own);
    } else rows.push(...own);
  }
  return rows;
}

// ---------------------------------------------------------------------------
// Dependencies

export type Dependency = { link: TimelineLink; from?: Schedule; to?: Schedule; conflict: boolean };

/**
 * dependencies pairs each "blocks" link with both schedules. A link conflicts when the
 * blocker is unfinished and ends on or after the day the blocked issue starts.
 * `warnings` collects a message per affected issue key.
 */
export function dependencies(data: Pick<TimelineData, "issues" | "links">, sched: Map<string, Schedule>) {
  const byKey = new Map(data.issues.map((i) => [i.key, i]));
  const warnings = new Map<string, string[]>();
  const warn = (key: string, msg: string) => warnings.set(key, [...(warnings.get(key) ?? []), msg]);
  const deps: Dependency[] = data.links.map((link) => {
    const from = sched.get(link.sourceKey);
    const to = sched.get(link.targetKey);
    const blocker = byKey.get(link.sourceKey);
    const conflict = !!from && !!to && !!blocker && !isDone(blocker) && from.end >= to.start;
    if (conflict) {
      warn(link.targetKey, `Blocked by ${link.sourceKey}, which ends ${formatDay(from.end)} — after this starts (${formatDay(to.start)}).`);
      warn(link.sourceKey, `Blocks ${link.targetKey}, which starts ${formatDay(to.start)} — before this ends (${formatDay(from.end)}).`);
    }
    return { link, from, to, conflict };
  });
  return { deps, warnings };
}

// ---------------------------------------------------------------------------
// Header counts and quick filters

export function isOverdue(i: Issue, today = todayDay()) {
  return !!i.dueDate && toDay(i.dueDate) < today && !isDone(i);
}

export function stats(data: Pick<TimelineData, "issues">, sched: Map<string, Schedule>, deps: Dependency[], today = todayDay()) {
  let inProgress = 0;
  let overdue = 0;
  let unscheduled = 0;
  for (const i of data.issues) {
    if (i.status.category === "in_progress") inProgress++;
    if (isOverdue(i, today)) overdue++;
    if (!sched.has(i.key)) unscheduled++;
  }
  return { inProgress, overdue, unscheduled, conflicts: deps.filter((d) => d.conflict).length };
}

/** epicProgress counts done children per epic from a list of child issues. */
export function epicProgress(children: Pick<Issue, "parent" | "status">[]) {
  const out = new Map<string, { done: number; total: number }>();
  for (const c of children) {
    if (!c.parent) continue;
    const p = out.get(c.parent.key) ?? { done: 0, total: 0 };
    p.total++;
    if (isDone(c)) p.done++;
    out.set(c.parent.key, p);
  }
  return out;
}

/** withDates returns the timeline with one issue's dates replaced (for optimistic edits). */
export function withDates<T extends Pick<TimelineData, "issues">>(data: T, key: string, startDate: string | null, dueDate: string | null, version?: number): T {
  return {
    ...data,
    issues: data.issues.map((i) =>
      i.key === key ? { ...i, startDate: startDate ?? undefined, dueDate: dueDate ?? undefined, version: version ?? i.version } : i,
    ),
  };
}
