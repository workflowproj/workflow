import { describe, expect, it } from "vitest";
import type { Issue } from "@/api/types";
import { buildProgrammeRows, buildRows, dependencies, epicProgress, externalWarnings, rollupProgress, fromDay, isOverdue, schedules, stats, toDay, todayDay, weekday, withDates, type TimelineData } from "./model";

const typeNames = ["Story", "Epic", "Initiative"];
const type = (level: number) => ({ id: `t${level}`, key: typeNames[level]?.toLowerCase() ?? "story", name: typeNames[level] ?? "Story", icon: "story", color: "#000", level, kind: "work" as const });
const status = (category: "todo" | "in_progress" | "done" = "todo") => ({ id: `s-${category}`, name: category, category });
const ref = (i: Issue) => ({ id: i.id, key: i.key, summary: i.summary, type: i.type, status: i.status });

function issue(key: string, over: Partial<Issue> = {}): Issue {
  return {
    id: key, key, projectKey: key.split("-")[0], type: type(0), summary: key, status: status(), priority: "medium",
    labels: [], rank: key, version: 1, fields: {}, createdAt: "", updatedAt: "", ...over,
  };
}

const sprint = { projectKey: "P", sprint: { id: "sp1", name: "Sprint 1", goal: "", state: "active" as const, position: 1, startDate: "2026-10-20", endDate: "2026-11-02" } };

describe("days", () => {
  it("round-trips ISO dates and knows weekdays", () => {
    expect(fromDay(toDay("2026-10-24"))).toBe("2026-10-24");
    expect(toDay("2026-11-01") - toDay("2026-10-31")).toBe(1);
    expect(weekday(toDay("2026-10-24"))).toBe(6); // Saturday
    expect(weekday(toDay("2026-10-26"))).toBe(1);
    expect(fromDay(todayDay(new Date(2026, 9, 24, 23, 30)))).toBe("2026-10-24");
  });
});

describe("schedules", () => {
  const epic = issue("P-1", { type: type(1) });
  const data = {
    sprints: [sprint],
    issues: [
      epic,
      issue("P-2", { parent: ref(epic), startDate: "2026-10-01", dueDate: "2026-10-10" }),
      issue("P-3", { parent: ref(epic), sprintId: "sp1" }),
      issue("P-4", { dueDate: "2026-12-01" }),
      issue("P-5", { startDate: "2026-12-05" }),
      issue("P-6"),
      issue("P-7", { startDate: "2026-10-10", dueDate: "2026-10-01" }),
    ],
  };
  const s = schedules(data);
  it("uses an issue's own dates", () => {
    expect(s.get("P-2")).toEqual({ start: toDay("2026-10-01"), end: toDay("2026-10-10"), source: "own" });
    expect(s.get("P-7")?.start).toBe(toDay("2026-10-01"));
  });
  it("derives from the sprint, children and lone dates", () => {
    expect(s.get("P-3")).toMatchObject({ start: toDay("2026-10-20"), end: toDay("2026-11-02"), source: "sprint" });
    expect(s.get("P-1")).toMatchObject({ start: toDay("2026-10-01"), end: toDay("2026-11-02"), source: "children" });
    expect(s.get("P-4")).toMatchObject({ start: toDay("2026-11-27"), end: toDay("2026-12-01"), source: "due" });
    expect(s.get("P-5")).toMatchObject({ start: toDay("2026-12-05"), source: "start" });
    expect(s.get("P-1")?.note).toMatch(/spans its 2 scheduled issues/);
  });
  it("leaves undated issues out", () => {
    expect(s.has("P-6")).toBe(false);
  });
  it("prefers an epic's own dates", () => {
    const own = schedules({ sprints: [], issues: [{ ...epic, startDate: "2026-01-01", dueDate: "2026-02-01" }, ...data.issues.slice(1)] });
    expect(own.get("P-1")?.source).toBe("own");
  });
});

describe("buildRows", () => {
  const e1 = issue("A-1", { type: type(1) });
  const gone = issue("A-9", { type: type(1), summary: "Filtered epic" });
  const data: Pick<TimelineData, "projects" | "issues"> = {
    projects: [
      { id: "a", key: "A", name: "Alpha" },
      { id: "b", key: "B", name: "Beta" },
    ],
    issues: [e1, issue("A-2", { parent: ref(e1) }), issue("A-3"), issue("A-4", { parent: ref(gone) }), issue("B-1")],
  };
  it("nests epics, orphans and loose issues per project", () => {
    const rows = buildRows(data, { multi: true, collapsed: new Set() });
    expect(rows.map((r) => r.id)).toEqual(["project:A", "A-1", "A-2", "A:parent:A-9", "A-4", "A:none", "A-3", "project:B", "B:none", "B-1"]);
    expect(rows.find((r) => r.id === "A-2")).toMatchObject({ depth: 2 });
    expect(rows[0]).toMatchObject({ count: 4 });
  });
  it("collapses and filters", () => {
    const rows = buildRows(data, { multi: false, collapsed: new Set(["A-1"]), include: (i) => i.key !== "A-3" });
    expect(rows.map((r) => r.id)).toEqual(["A-1", "A:parent:A-9", "A-4", "B:none", "B-1"]);
    const onlyChild = buildRows(data, { multi: false, collapsed: new Set(), include: (i) => i.key === "A-2" });
    expect(onlyChild.map((r) => r.id)).toEqual(["A-1", "A-2"]);
    const filtered = buildRows(data, { multi: true, collapsed: new Set(), include: (i) => i.key === "A-2", hideEmpty: true });
    expect(filtered.map((r) => r.id)).toEqual(["project:A", "A-1", "A-2"]);
  });
});

describe("dependencies and stats", () => {
  const a = issue("P-1", { startDate: "2026-10-01", dueDate: "2026-10-10", status: status("in_progress") });
  const b = issue("P-2", { startDate: "2026-10-10", dueDate: "2026-10-20" });
  const c = issue("P-3", { startDate: "2026-10-11", dueDate: "2026-10-20" });
  const done = issue("P-4", { startDate: "2026-10-01", dueDate: "2026-10-30", status: status("done") });
  const data = {
    issues: [a, b, c, done, issue("P-5", { dueDate: "2026-10-01" }), issue("P-6")],
    sprints: [],
    links: [
      { id: "1", sourceKey: "P-1", targetKey: "P-2", type: "blocks" as const },
      { id: "2", sourceKey: "P-1", targetKey: "P-3", type: "blocks" as const },
      { id: "3", sourceKey: "P-4", targetKey: "P-3", type: "blocks" as const },
      { id: "4", sourceKey: "P-1", targetKey: "P-6", type: "blocks" as const },
    ],
  };
  const sched = schedules(data);
  const { deps, warnings } = dependencies(data, sched);
  it("flags a blocker ending on or after the blocked start, unless it is done", () => {
    expect(deps.map((d) => d.conflict)).toEqual([true, false, false, false]);
    expect(warnings.get("P-2")?.[0]).toMatch(/Blocked by P-1/);
    expect(warnings.get("P-1")?.[0]).toMatch(/Blocks P-2/);
    expect(warnings.has("P-3")).toBe(false);
  });
  it("counts the header figures", () => {
    const today = toDay("2026-10-15");
    expect(isOverdue(data.issues[4], today)).toBe(true);
    expect(stats(data, sched, deps, today)).toEqual({ inProgress: 1, overdue: 2, unscheduled: 1, conflicts: 1 });
  });
});

describe("helpers", () => {
  it("computes epic progress", () => {
    const p = epicProgress([
      { parent: { id: "e", key: "E-1", summary: "", type: type(1), status: status() }, status: status("done") },
      { parent: { id: "e", key: "E-1", summary: "", type: type(1), status: status() }, status: status() },
      { status: status() },
    ]);
    expect(p.get("E-1")).toEqual({ done: 1, total: 2 });
  });
  it("replaces dates optimistically", () => {
    const out = withDates({ issues: [issue("P-1", { dueDate: "2026-01-01" })] }, "P-1", "2026-02-01", null, 7);
    expect(out.issues[0]).toMatchObject({ startDate: "2026-02-01", dueDate: undefined, version: 7 });
  });
});

describe("programmes", () => {
  const init = issue("PROG-1", { type: type(2) });
  const appEpic = issue("APP-1", { type: type(1), parent: ref(init) });
  const apiEpic = issue("API-1", { type: type(1), parent: ref(init), startDate: "2026-12-01", dueDate: "2026-12-20" });
  const lone = issue("APP-3", { type: type(1) });
  const data = {
    projects: [
      { id: "prog", key: "PROG", name: "Programme" },
      { id: "app", key: "APP", name: "App" },
      { id: "api", key: "API", name: "Api" },
    ],
    sprints: [],
    issues: [
      init,
      appEpic,
      issue("APP-2", { parent: ref(appEpic), startDate: "2026-11-02", dueDate: "2026-11-13" }),
      apiEpic,
      lone,
      issue("APP-4", { parent: ref(lone) }),
      issue("APP-5"),
      issue("PROG-2"),
    ],
  };

  it("rolls dates up from issues to epics to the initiative", () => {
    const s = schedules(data);
    expect(s.get("APP-1")).toMatchObject({ start: toDay("2026-11-02"), end: toDay("2026-11-13"), source: "children" });
    // The initiative spans its epics: one derived from its story, one with its own dates.
    expect(s.get("PROG-1")).toMatchObject({ start: toDay("2026-11-02"), end: toDay("2026-12-20"), source: "children" });
  });

  it("nests initiative, team epics and their issues, then the rest by project", () => {
    const rows = buildProgrammeRows(data, { collapsed: new Set() });
    expect(rows.map((r) => (r.kind === "issue" ? `${r.depth}:${r.id}` : r.kind === "project" ? `project:${r.project.key}` : `group:${r.label}`))).toEqual([
      "0:PROG-1",
      "1:APP-1",
      "2:APP-2",
      "1:API-1",
      "project:PROG",
      "group:No epic",
      "2:PROG-2",
      "project:APP",
      "1:APP-3",
      "2:APP-4",
      "group:No epic",
      "2:APP-5",
    ]);
    expect(rows[0]).toMatchObject({ childCount: 2 });
    // Collapsing the initiative hides everything under it.
    const collapsed = buildProgrammeRows(data, { collapsed: new Set(["PROG-1"]) });
    expect(collapsed.filter((r) => r.kind === "issue" && r.depth > 0 && ["APP-1", "API-1", "APP-2"].includes(r.id))).toEqual([]);
  });

  it("fills an initiative with the work under its epics", () => {
    const fill = rollupProgress(data.issues, new Map([["PROG-1", { done: 0, total: 2 }], ["APP-1", { done: 1, total: 4 }], ["API-1", { done: 2, total: 3 }]]));
    expect(fill.get("PROG-1")).toEqual({ done: 3, total: 7 });
    expect(fill.get("APP-1")).toEqual({ done: 1, total: 4 });
  });

  it("explains dependencies on issues outside the timeline, naming only visible ones", () => {
    const w = externalWarnings({
      externalLinks: [
        { issueKey: "APP-2", direction: "blockedBy", open: true, other: issue("OPS-9", { summary: "PCI scan" }) },
        { issueKey: "APP-2", direction: "blocks", open: true },
        { issueKey: "API-1", direction: "blockedBy", open: false },
      ],
    });
    expect(w.get("APP-2")).toEqual(["Blocked by OPS-9 (PCI scan), which isn't on this timeline.", "Blocks an unfinished issue in a project you can't browse."]);
    expect(w.has("API-1")).toBe(false);
  });
});
