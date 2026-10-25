import type { Issue, User } from "@/api/types";

/** The people filter's id for issues without an assignee. */
export const UNASSIGNED = "none";

/** A person the board can be filtered by: an assignee (or "Unassigned") and how many issues they have. */
export type Person = { id: string; user: User | null; count: number };

/** personId is the people-filter id of an issue's assignee. */
export function personId(issue: Issue) {
  return issue.assignee?.id ?? UNASSIGNED;
}

/**
 * peopleOf lists the assignees of `issues`: you first, then by number of issues and name, with
 * "Unassigned" last. Selected people who no longer have issues stay in the list (with 0) so they
 * can be deselected; `known` supplies their names.
 */
export function peopleOf(issues: Iterable<Issue>, meId?: string, selected: string[] = [], known?: Map<string, User>): Person[] {
  const byId = new Map<string, Person>();
  for (const issue of issues) {
    const id = personId(issue);
    const p = byId.get(id);
    if (p) p.count++;
    else byId.set(id, { id, user: issue.assignee ?? null, count: 1 });
  }
  for (const id of selected) {
    if (byId.has(id)) continue;
    const user = known?.get(id);
    if (id === UNASSIGNED || user) byId.set(id, { id, user: user ?? null, count: 0 });
  }
  const rank = (p: Person) => (p.id === UNASSIGNED ? 2 : p.id === meId ? 0 : 1);
  return [...byId.values()].sort((a, b) => rank(a) - rank(b) || b.count - a.count || (a.user?.displayName ?? "").localeCompare(b.user?.displayName ?? ""));
}

/** matchesPeople keeps every issue when nobody is selected, otherwise issues assigned to a selected person. */
export function matchesPeople(issue: Issue, selected: string[]) {
  return selected.length === 0 || selected.includes(personId(issue));
}

/**
 * filterColumns applies the people filter to board columns. `total` keeps the column's full
 * count so work-in-progress limits still describe the whole team.
 */
export function filterColumns<C extends { issues: Issue[] }>(columns: C[], selected: string[]): (C & { total?: number })[] {
  if (selected.length === 0) return columns;
  return columns.map((c) => ({ ...c, issues: c.issues.filter((i) => matchesPeople(i, selected)), total: c.issues.length }));
}

/** filterBacklog applies the people filter to the backlog and its sprints. */
export function filterBacklog<T extends { sprints: { issues: Issue[] }[]; backlog: Issue[] }>(data: T, selected: string[]): T {
  if (selected.length === 0) return data;
  const keep = (list: Issue[]) => list.filter((i) => matchesPeople(i, selected));
  return { ...data, sprints: data.sprints.map((s) => ({ ...s, issues: keep(s.issues) })), backlog: keep(data.backlog) };
}
