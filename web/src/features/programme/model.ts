import type { Schemas } from "@/api/client";

/*
 * Pure roadmap logic: grouping an initiative's epics by team, progress wording and totals, and
 * the cross-team dependencies between a programme's projects.
 */

type Issue = Schemas["Issue"];
type Progress = Schemas["IssueProgress"];
type Flag = Schemas["PlanningFlag"];
type ProgrammeEpic = Schemas["ProgrammeEpic"];
type ProgrammeOverview = Schemas["ProgrammeOverview"];
type TimelineLink = Schemas["TimelineLink"];

/** groupByTeam splits epics into one group per project, in the order the projects first appear. */
export function groupByTeam(epics: ProgrammeEpic[]): { projectKey: string; epics: ProgrammeEpic[] }[] {
  const out: { projectKey: string; epics: ProgrammeEpic[] }[] = [];
  for (const e of epics) {
    const g = out.find((x) => x.projectKey === e.issue.projectKey);
    if (g) g.epics.push(e);
    else out.push({ projectKey: e.issue.projectKey, epics: [e] });
  }
  return out;
}

/** progressText reads "3 of 8 done · 5 of 21 pts" (points only when there are some). */
export function progressText(p: Progress): string {
  if (p.total === 0) return "No issues yet";
  const issues = `${p.done} of ${p.total} done`;
  return p.points > 0 ? `${issues} · ${fmt(p.donePoints)} of ${fmt(p.points)} pts` : issues;
}

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1));

/** donePercent is the share of issues done, rounded (0 when there are none). */
export function donePercent(p: Progress): number {
  return p.total === 0 ? 0 : Math.round((p.done / p.total) * 100);
}

/** forecastLate reports a forecast after the due date (both ISO dates). */
export function forecastLate(due: string | undefined, forecast: string | undefined): boolean {
  return !!due && !!forecast && forecast > due;
}

export const flagLabel: Record<Flag["kind"], string> = { late: "Late", overdue: "Overdue", unscheduled: "Unscheduled", blocked: "Blocked" };

/** roadmapTotals counts what the header shows. */
export function roadmapTotals(o: ProgrammeOverview) {
  let epics = 0;
  let open = 0;
  let late = 0;
  let blocked = 0;
  for (const i of o.initiatives) {
    if (i.issue.status.category !== "done") open++;
    epics += i.epics.length;
    for (const e of i.epics) {
      if (e.flags.some((f) => f.kind === "late")) late++;
      if (e.flags.some((f) => f.kind === "blocked")) blocked++;
    }
  }
  return { initiatives: o.initiatives.length, open, epics, late, blocked };
}

// ---------------------------------------------------------------------------
// Dependencies between teams

export type CrossTeamLink = { link: TimelineLink; from: Issue; to: Issue };

/**
 * crossTeamLinks keeps the blocks links whose two issues are in different projects; matrix counts
 * them by blocking project (rows) and blocked project (columns).
 */
export function crossTeamLinks(data: { issues: Issue[]; links: TimelineLink[] }): CrossTeamLink[] {
  const byKey = new Map(data.issues.map((i) => [i.key, i]));
  const out: CrossTeamLink[] = [];
  for (const link of data.links) {
    const from = byKey.get(link.sourceKey);
    const to = byKey.get(link.targetKey);
    if (from && to && from.projectKey !== to.projectKey) out.push({ link, from, to });
  }
  return out;
}

export function dependencyMatrix(links: CrossTeamLink[]): Map<string, Map<string, CrossTeamLink[]>> {
  const m = new Map<string, Map<string, CrossTeamLink[]>>();
  for (const l of links) {
    const row = m.get(l.from.projectKey) ?? new Map<string, CrossTeamLink[]>();
    row.set(l.to.projectKey, [...(row.get(l.to.projectKey) ?? []), l]);
    m.set(l.from.projectKey, row);
  }
  return m;
}
