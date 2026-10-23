import type { Schemas } from "@/api/client";
import { PRIORITIES, type Issue, type Priority, type User } from "@/api/types";

/*
 * Pure helpers behind the board and backlog: swimlane grouping, drag-and-drop container
 * bookkeeping, rank neighbours for POST /issues/{key}/move and optimistic cache updates.
 */

export type SwimlaneMode = Schemas["Swimlanes"];
type IssueRef = Schemas["IssueRef"];
type IssueTypeRef = Schemas["IssueTypeRef"];
type ColumnData = Schemas["BoardColumnData"];

export type Lane = {
  id: string;
  label: string;
  user?: User;
  epic?: IssueRef;
  priority?: Priority;
  type?: IssueTypeRef;
  /** Issues per column id, in rank order. */
  cells: Record<string, Issue[]>;
  count: number;
};

export const SWIMLANE_LABELS: Record<SwimlaneMode, string> = {
  none: "No swimlanes",
  assignee: "Assignee",
  epic: "Epic",
  priority: "Priority",
  type: "Issue type",
};

/** epicOf finds an issue's epic: its parent, or (for a sub-task) its parent's parent when that is on the board. */
export function epicOf(issue: Issue, byId: Map<string, Issue>): IssueRef | undefined {
  if (!issue.parent) return undefined;
  if (issue.parent.type.level >= 1) return issue.parent;
  const parent = byId.get(issue.parent.id);
  return parent?.parent && parent.parent.type.level >= 1 ? parent.parent : undefined;
}

type LaneKey = Omit<Lane, "cells" | "count"> & { order: [number, string] };

function laneKey(issue: Issue, mode: SwimlaneMode, byId: Map<string, Issue>, meId?: string): LaneKey {
  switch (mode) {
    case "assignee": {
      const u = issue.assignee;
      if (!u) return { id: "assignee:none", label: "Unassigned", order: [2, ""] };
      return { id: `assignee:${u.id}`, label: u.displayName, user: u, order: [u.id === meId ? 0 : 1, u.displayName.toLowerCase()] };
    }
    case "epic": {
      const e = epicOf(issue, byId);
      if (!e) return { id: "epic:none", label: "No epic", order: [2, ""] };
      const n = Number(e.key.split("-").pop());
      return { id: `epic:${e.id}`, label: e.summary, epic: e, order: [1, String(n).padStart(9, "0")] };
    }
    case "priority": {
      const p = issue.priority;
      return { id: `priority:${p}`, label: p[0].toUpperCase() + p.slice(1), priority: p, order: [PRIORITIES.indexOf(p), ""] };
    }
    case "type": {
      const t = issue.type;
      return { id: `type:${t.id}`, label: t.name, type: t, order: [1 - t.level, t.name.toLowerCase()] };
    }
    default:
      return { id: "all", label: "All issues", order: [0, ""] };
  }
}

/** buildLanes groups the board's columns into swimlanes; "none" gives a single lane (even when empty). */
export function buildLanes(columns: ColumnData[], mode: SwimlaneMode, meId?: string): Lane[] {
  const byId = new Map<string, Issue>();
  for (const c of columns) for (const i of c.issues) byId.set(i.id, i);
  const lanes = new Map<string, Lane & { order: [number, string] }>();
  const empty = () => Object.fromEntries(columns.map((c) => [c.id, [] as Issue[]]));
  if (mode === "none") lanes.set("all", { id: "all", label: "All issues", order: [0, ""], cells: empty(), count: 0 });
  for (const c of columns) {
    for (const issue of c.issues) {
      const k = mode === "none" ? laneKey(issue, "none", byId) : laneKey(issue, mode, byId, meId);
      let lane = lanes.get(k.id);
      if (!lane) {
        lane = { ...k, cells: empty(), count: 0 };
        lanes.set(k.id, lane);
      }
      lane.cells[c.id].push(issue);
      lane.count++;
    }
  }
  return [...lanes.values()]
    .sort((a, b) => a.order[0] - b.order[0] || a.order[1].localeCompare(b.order[1]))
    .map(({ order: _order, ...lane }) => lane);
}

// ---------------------------------------------------------------------------
// Drag-and-drop containers: container id -> ordered item ids.

export type Containers = Record<string, string[]>;

const SEP = "::";
export const cellId = (laneId: string, columnId: string) => `${laneId}${SEP}${columnId}`;
export function parseCellId(id: string): { laneId: string; columnId: string } {
  const i = id.lastIndexOf(SEP);
  return { laneId: id.slice(0, i), columnId: id.slice(i + SEP.length) };
}

export function findContainer(c: Containers, id: string): string | undefined {
  if (id in c) return id;
  return Object.keys(c).find((k) => c[k].includes(id));
}

/** moveAcross moves an item from one container to another at `index` (clamped). */
export function moveAcross(c: Containers, id: string, from: string, to: string, index: number): Containers {
  const source = c[from].filter((x) => x !== id);
  const target = c[to].filter((x) => x !== id);
  const at = Math.max(0, Math.min(index, target.length));
  return { ...c, [from]: source, [to]: [...target.slice(0, at), id, ...target.slice(at)] };
}

export function sameOrder(a: string[], b: string[]) {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

export type RankHint = { afterIssueId?: string; beforeIssueId?: string };

/** neighbours returns the rank hint for `id` in its final list: after the issue above it, else before the one below. */
export function neighbours(list: string[], id: string): RankHint {
  const i = list.indexOf(id);
  if (i < 0) return {};
  if (i > 0) return { afterIssueId: list[i - 1] };
  if (i + 1 < list.length) return { beforeIssueId: list[i + 1] };
  return {};
}

/** insertWithHint inserts an issue next to its rank neighbour, or by rank when no neighbour is in the list. */
export function insertWithHint(list: Issue[], issue: Issue, hint: RankHint): Issue[] {
  const out = list.filter((i) => i.id !== issue.id);
  let at = -1;
  if (hint.afterIssueId) {
    const j = out.findIndex((i) => i.id === hint.afterIssueId);
    if (j >= 0) at = j + 1;
  }
  if (at < 0 && hint.beforeIssueId) at = out.findIndex((i) => i.id === hint.beforeIssueId);
  if (at < 0) {
    at = out.findIndex((i) => i.rank > issue.rank);
    if (at < 0) at = out.length;
  }
  out.splice(at, 0, issue);
  return out;
}

/** placeOnBoard is the optimistic version of a board drop: the issue moves column (and status) and rank. */
export function placeOnBoard<T extends { columns: ColumnData[] }>(data: T, issueId: string, toColumnId: string, hint: RankHint, status?: Issue["status"]): T {
  let moving: Issue | undefined;
  for (const c of data.columns) moving ??= c.issues.find((i) => i.id === issueId);
  if (!moving) return data;
  const issue = status ? { ...moving, status } : moving;
  const columns = data.columns.map((c) => {
    let issues = c.issues.filter((i) => i.id !== issueId);
    if (c.id === toColumnId) issues = insertWithHint(issues, issue, hint);
    return { ...c, issues, overLimit: c.wipLimit != null && issues.length > c.wipLimit };
  });
  return { ...data, columns };
}

/** The backlog's container id next to sprint ids. */
export const BACKLOG = "backlog";

/** placeInBacklog moves an issue between sprints and the backlog. */
export function placeInBacklog<T extends { sprints: { sprint: { id: string }; issues: Issue[] }[]; backlog: Issue[] }>(
  data: T,
  issueId: string,
  to: string,
  hint: RankHint,
): T {
  const all = [...data.backlog, ...data.sprints.flatMap((s) => s.issues)];
  const moving = all.find((i) => i.id === issueId);
  if (!moving) return data;
  const issue = { ...moving, sprintId: to === BACKLOG ? undefined : to };
  const place = (list: Issue[], id: string) => (id === to ? insertWithHint(list, issue, hint) : list.filter((i) => i.id !== issueId));
  return {
    ...data,
    sprints: data.sprints.map((s) => ({ ...s, issues: place(s.issues, s.sprint.id) })),
    backlog: place(data.backlog, BACKLOG),
  };
}


/** sumPoints adds up story points, split by status category. */
export function sumPoints(issues: Issue[]) {
  const out = { total: 0, todo: 0, in_progress: 0, done: 0, estimated: 0 };
  for (const i of issues) {
    const p = i.storyPoints ?? 0;
    out.total += p;
    out[i.status.category] += p;
    if (i.storyPoints != null) out.estimated++;
  }
  return out;
}

/** formatPoints prints 3, 2.5 or 0.25 without trailing zeros. */
export function formatPoints(n: number) {
  return String(Math.round(n * 100) / 100);
}
