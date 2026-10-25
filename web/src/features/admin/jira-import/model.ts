import type { Schemas } from "@/api/client";

/*
 * The Import from Jira wizard: which step an import is at, defaults for new targets, and
 * presentation of mappings, progress and reports. Pure functions so they are unit-tested.
 */

export type JiraImport = Schemas["JiraImport"];
export type JiraConfig = Schemas["JiraImportConfig"];
export type JiraTarget = Schemas["JiraProjectTarget"];
export type JiraProject = Schemas["JiraProject"];
export type JiraPerson = Schemas["JiraPerson"];
export type PersonMapping = Schemas["JiraPersonMapping"];
export type Problem = Schemas["JiraImportProblem"];
export type DryRun = Schemas["JiraDryRun"];
export type Progress = Schemas["JiraImportProgress"];

export const STEPS = [
  { key: "connect", label: "Connect" },
  { key: "projects", label: "Projects" },
  { key: "mapping", label: "Mapping" },
  { key: "dryrun", label: "Dry run" },
  { key: "import", label: "Import" },
] as const;
export type StepKey = (typeof STEPS)[number]["key"];

export const TEMPLATES = [
  { id: "", label: "From Jira boards" },
  { id: "scrum", label: "Scrum" },
  { id: "kanban", label: "Kanban" },
  { id: "bug_tracking", label: "Bug tracking" },
] as const;

/** busy reports a scan or import that is running (and checkpointing). */
export function busy(imp: Pick<JiraImport, "status" | "stalled">): boolean {
  return (imp.status === "scanning" || imp.status === "running") && !imp.stalled;
}

/** currentStep is the step to open an import at: the import once one ran, else where setup stands. */
export function currentStep(imp: Pick<JiraImport, "mode" | "config">): StepKey {
  if (imp.mode === "import") return "import";
  if (imp.config.projects.length === 0) return "projects";
  return "mapping";
}

/** reachable reports whether the admin may open a step. */
export function reachable(imp: Pick<JiraImport, "status" | "mode" | "config" | "scanStale">, step: StepKey): boolean {
  switch (step) {
    case "connect":
    case "projects":
      return true;
    case "mapping":
      return imp.config.projects.length > 0;
    case "dryrun":
      return imp.config.projects.length > 0 && !imp.scanStale;
    case "import":
      return imp.mode === "import";
  }
}

/** sanitizeKey mirrors the server's default key: uppercase letters and digits, 2-10, a letter first. */
export function sanitizeKey(jiraKey: string): string {
  let k = jiraKey.toUpperCase().replace(/[^A-Z0-9]/g, "");
  k = k.replace(/^[0-9]+/, "");
  if (k.length > 10) k = k.slice(0, 10);
  while (k.length < 2) k += "X";
  return k;
}

export function keyError(key: string): string | undefined {
  if (!/^[A-Z][A-Z0-9]{1,9}$/.test(key)) return "2-10 uppercase letters or digits, starting with a letter";
  return undefined;
}

export function newTarget(p: JiraProject): JiraTarget {
  return { jiraId: p.id, jiraKey: p.key, jiraName: p.name, key: sanitizeKey(p.key), name: p.name, template: "", open: true, statuses: {}, types: {} };
}

/** toggleProject adds or removes a Jira project from the selection, keeping other targets. */
export function toggleProject(cfg: JiraConfig, p: JiraProject): JiraConfig {
  const has = cfg.projects.some((t) => t.jiraId === p.id);
  return { ...cfg, projects: has ? cfg.projects.filter((t) => t.jiraId !== p.id) : [...cfg.projects, newTarget(p)] };
}

export function updateTarget(cfg: JiraConfig, jiraId: string, patch: Partial<JiraTarget>): JiraConfig {
  return { ...cfg, projects: cfg.projects.map((t) => (t.jiraId === jiraId ? { ...t, ...patch } : t)) };
}

export function setStatus(cfg: JiraConfig, jiraId: string, statusId: string, name: string): JiraConfig {
  return { ...cfg, projects: cfg.projects.map((t) => (t.jiraId === jiraId ? { ...t, statuses: { ...t.statuses, [statusId]: name } } : t)) };
}

export function setType(cfg: JiraConfig, jiraId: string, typeId: string, key: string): JiraConfig {
  return { ...cfg, projects: cfg.projects.map((t) => (t.jiraId === jiraId ? { ...t, types: { ...t.types, [typeId]: key } } : t)) };
}

export function setPerson(cfg: JiraConfig, personId: string, m: PersonMapping | null): JiraConfig {
  const people = { ...cfg.people };
  if (!m || m.action === "auto") delete people[personId];
  else people[personId] = m;
  return { ...cfg, people };
}

/** personOutcome says in words what an import does with a person. */
export function personOutcome(p: JiraPerson, m: PersonMapping | undefined): { tone: "success" | "accent" | "warning" | "neutral"; text: string } {
  const action = m?.action ?? "auto";
  if (action === "skip") return { tone: "neutral", text: "Left empty" };
  if (action === "user") return { tone: "success", text: "Mapped to the chosen account" };
  const email = (action === "placeholder" && m?.email) || (action === "auto" && p.match ? "" : p.email);
  if (action === "auto" && p.match) {
    return p.placeholder ? { tone: "accent", text: `Placeholder from an earlier import (${p.match.email})` } : { tone: "success", text: `Matched: ${p.match.displayName}` };
  }
  if (email) return { tone: "accent", text: `Placeholder; claimed when ${email} signs in` };
  return { tone: "warning", text: "Placeholder nobody can claim (no email)" };
}

const severityRank: Record<Problem["severity"], number> = { error: 0, warning: 1, info: 2 };

export function sortProblems(list: Problem[]): Problem[] {
  return [...list].sort((a, b) => severityRank[a.severity] - severityRank[b.severity]);
}

export function problemCounts(list: Problem[]): Record<Problem["severity"], number> {
  const out = { error: 0, warning: 0, info: 0 };
  for (const p of list) out[p.severity]++;
  return out;
}

/** phasePercent is a phase's completion, 0-100 (done phases are 100). */
export function phasePercent(ph: Progress["phases"][number]): number {
  if (ph.status === "done") return 100;
  if (ph.total <= 0) return ph.status === "running" ? 5 : 0;
  return Math.min(99, Math.round((ph.done / ph.total) * 100));
}

export function overallPercent(p: Progress | undefined): number {
  if (!p || p.phases.length === 0) return 0;
  return Math.round(p.phases.reduce((sum, ph) => sum + phasePercent(ph), 0) / p.phases.length);
}

const COUNTER_LABELS: [string, string][] = [
  ["projectsCreated", "Projects created"],
  ["issuesCreated", "Issues created"],
  ["issuesUpdated", "Issues updated"],
  ["issuesUnchanged", "Issues unchanged"],
  ["issuesSkipped", "Issues skipped"],
  ["comments", "Comments"],
  ["historyEntries", "History entries"],
  ["attachments", "Attachments"],
  ["attachmentsSkipped", "Attachments skipped"],
  ["sprints", "Sprints created"],
  ["releases", "Releases created"],
  ["parents", "Parent links"],
  ["links", "Issue links"],
  ["peopleMatched", "People matched"],
  ["placeholders", "Placeholders created"],
];

/** reportRows turns run counters into labelled rows, in a stable order, skipping zeros. */
export function reportRows(counters: Record<string, number> | undefined): { key: string; label: string; value: number }[] {
  if (!counters) return [];
  return COUNTER_LABELS.filter(([k]) => (counters[k] ?? 0) > 0).map(([key, label]) => ({ key, label, value: counters[key] }));
}

export const STATUS_LABEL: Record<JiraImport["status"], string> = {
  draft: "Draft",
  scanning: "Analysing",
  scanned: "Ready to map",
  running: "Importing",
  succeeded: "Imported",
  failed: "Failed",
  cancelled: "Cancelled",
};

export const STATUS_TONE: Record<JiraImport["status"], "neutral" | "accent" | "success" | "warning" | "danger"> = {
  draft: "neutral",
  scanning: "accent",
  scanned: "neutral",
  running: "accent",
  succeeded: "success",
  failed: "danger",
  cancelled: "warning",
};

export const LEVEL_LABEL: Record<number, string> = { 1: "Epic level", 0: "Standard", [-1]: "Sub-task" };

export const CATEGORY_LABEL: Record<string, string> = { todo: "To do", in_progress: "In progress", done: "Done" };

/** plural formats a count with a singular or plural noun ("1 warning", "2 warnings"). */
export function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

/** siteName is a short name for a Jira site URL. */
export function siteName(url: string): string {
  try {
    const u = new URL(url);
    return u.host + (u.pathname === "/" ? "" : u.pathname);
  } catch {
    return url;
  }
}
