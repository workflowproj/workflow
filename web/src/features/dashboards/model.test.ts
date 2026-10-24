import { describe, expect, it } from "vitest";
import {
  breakdownClause,
  cleanConfig,
  defaultTitle,
  duplicateGadget,
  formatStat,
  moveItem,
  newGadget,
  newGadgetId,
  pickSprint,
  sameDashboard,
  scopeJql,
  scopeSummary,
  searchJql,
  seedScope,
  statClause,
  statValue,
  type Gadget,
  type MetricsSummary,
} from "./model";

const gadget = (over: Partial<Gadget>): Gadget => ({ id: "a", type: "stat", width: 1, config: {}, ...over });

describe("scopeJql", () => {
  it("combines projects, JQL and extra clauses, keeping ORDER BY last", () => {
    expect(scopeJql(["PLAT"], "assignee = currentUser() ORDER BY due ASC", "statusCategory != Done")).toBe(
      "project = PLAT AND assignee = currentUser() AND statusCategory != Done ORDER BY due ASC",
    );
    expect(scopeJql(["A", "B"], "")).toBe("project in (A, B)");
    expect(scopeJql(undefined, "  ")).toBe("");
    expect(scopeJql([], "type = Bug")).toBe("type = Bug");
  });
  it("parenthesises parts with OR", () => {
    expect(scopeJql(["PLAT"], "type = Bug OR priority = High")).toBe("project = PLAT AND (type = Bug OR priority = High)");
    // OR inside a string is not an operator.
    expect(scopeJql(["PLAT"], 'summary ~ "this or that"')).toBe('project = PLAT AND summary ~ "this or that"');
  });
});

describe("breakdownClause", () => {
  it("builds the JQL for a group", () => {
    expect(breakdownClause("assignee", "4f1c")).toBe('assignee = "4f1c"');
    expect(breakdownClause("assignee", "")).toBe("assignee is EMPTY");
    expect(breakdownClause("label", "")).toBe("labels is EMPTY");
    expect(breakdownClause("statusCategory", "in_progress")).toBe('statusCategory = "In Progress"');
    expect(breakdownClause("statusCategory", "done")).toBe("statusCategory = Done");
    expect(breakdownClause("epic", "PLAT-4")).toBe('epic = "PLAT-4"');
    expect(breakdownClause("release", "r1")).toBe('fixVersion = "r1"');
    expect(breakdownClause("project", "PLAT")).toBe("project = PLAT");
  });
});

describe("stats", () => {
  const s: MetricsSummary = { days: 30, open: 12, todo: 7, inProgress: 5, created: 9, resolved: 4, overdue: 2, unassigned: 3, openPoints: 21.5, resolvedPoints: 8, medianCycleHours: 30 };
  it("reads and formats values", () => {
    expect(statValue(s, "in_progress")).toBe(5);
    expect(formatStat("median_cycle_hours", statValue(s, "median_cycle_hours"))).toBe("30h");
    expect(formatStat("open_points", 21.5)).toBe("21.5");
    expect(formatStat("median_cycle_hours", statValue({ ...s, medianCycleHours: undefined }, "median_cycle_hours"))).toBe("–");
  });
  it("links each stat to the issues it counts", () => {
    expect(statClause("overdue", 30)).toBe("statusCategory != Done AND due < startOfDay()");
    expect(statClause("resolved", 14)).toBe("resolved >= -14d");
    expect(searchJql(gadget({ config: { projectKeys: ["PLAT"], stat: "created", days: 7 } }))).toBe("project = PLAT AND created >= -7d");
  });
});

describe("titles and scope", () => {
  it("derives titles from type and settings", () => {
    expect(defaultTitle(gadget({ type: "sprint_burndown", config: { projectKey: "PLAT" } }), "Sprint 4")).toBe("Burndown · PLAT Sprint 4");
    expect(defaultTitle(gadget({ type: "sprint_burnup", config: {} }))).toBe("Burnup");
    expect(defaultTitle(gadget({ type: "sprint_health", config: { projectKey: "PLAT" } }), "PLAT Sprint 4")).toBe("Sprint health · PLAT Sprint 4");
    expect(defaultTitle(gadget({ type: "breakdown", config: { by: "priority", measure: "points", projectKeys: ["A", "B"] } }))).toBe("Points by priority · A, B");
    expect(defaultTitle(gadget({ type: "cumulative_flow", config: { projectKeys: ["A", "B", "C", "D"] } }))).toBe("Cumulative flow · 4 projects");
    expect(defaultTitle(gadget({ type: "stat", config: { stat: "median_cycle_hours" } }))).toBe("Median cycle time");
  });
  it("summarises the scope", () => {
    expect(scopeSummary(gadget({ type: "created_resolved", config: { jql: "type = Bug ORDER BY created", days: 90, interval: "week" } }))).toBe("All projects · type = Bug · last 90 days, weekly");
    expect(scopeSummary(gadget({ type: "sprint_health", config: { projectKey: "PLAT" } }))).toBe("PLAT · active sprint");
    expect(scopeSummary(gadget({ type: "velocity", config: { projectKey: "PLAT", count: 5 } }))).toBe("PLAT · last 5 completed sprints");
    expect(searchJql(gadget({ type: "velocity", config: { projectKey: "PLAT" } }))).toBeNull();
  });
});

describe("pickSprint", () => {
  const sp = (id: string, state: "future" | "active" | "closed", completedAt?: string) => ({ id, name: id, goal: "", state, position: 0, completedAt });
  it("prefers the chosen sprint, then the active one, then the last completed", () => {
    const list = [sp("old", "closed", "2026-09-01T00:00:00Z"), sp("recent", "closed", "2026-10-01T00:00:00Z"), sp("next", "future")];
    expect(pickSprint(list)?.id).toBe("recent");
    expect(pickSprint([...list, sp("now", "active")])?.id).toBe("now");
    expect(pickSprint(list, "old")?.id).toBe("old");
    expect(pickSprint(list, "gone")).toBeUndefined();
    expect(pickSprint([sp("next", "future")])).toBeUndefined();
  });
});

describe("editing", () => {
  it("keeps only the settings a type uses", () => {
    expect(cleanConfig("velocity", { projectKey: "A", sprintId: "x", projectKeys: ["B"], count: 5, days: 30 })).toEqual({ projectKey: "A", count: 5 });
    expect(cleanConfig("stat", { stat: "open", days: 30, jql: " " })).toEqual({ stat: "open" });
    expect(cleanConfig("stat", { stat: "resolved", days: 30, projectKeys: [] })).toEqual({ stat: "resolved", days: 30 });
  });
  it("makes unique ids and new gadgets with defaults", () => {
    const existing = [gadget({ id: "g-000000" })];
    let calls = 0;
    expect(newGadgetId(existing, () => (calls++ === 0 ? 0 : 0.5))).not.toBe("g-000000");
    const g = newGadget("cumulative_flow", existing, { projectKey: "A", projectKeys: ["A"] });
    expect(g).toMatchObject({ type: "cumulative_flow", width: 2, config: { projectKeys: ["A"], days: 30, flowBy: "category" } });
    expect(newGadget("sprint_burndown", [], { projectKey: "A", projectKeys: ["A"] }).config).toEqual({ projectKey: "A" });
  });
  it("moves and duplicates gadgets", () => {
    expect(moveItem(["a", "b", "c"], 0, 2)).toEqual(["b", "c", "a"]);
    expect(moveItem(["a", "b"], 1, 5)).toEqual(["a", "b"]);
    const list = [gadget({ id: "a", title: "One" }), gadget({ id: "b" })];
    const out = duplicateGadget(list, "a");
    expect(out.map((g) => g.title)).toEqual(["One", "One", undefined]);
    expect(new Set(out.map((g) => g.id)).size).toBe(3);
  });
  it("seeds a new gadget's projects from the dashboard", () => {
    expect(seedScope([gadget({ config: { projectKey: "PLAT" } })])).toEqual({ projectKey: "PLAT", projectKeys: ["PLAT"] });
    expect(seedScope([gadget({ config: { projectKeys: ["A", "B"] } })])).toEqual({ projectKey: "A", projectKeys: ["A", "B"] });
    expect(seedScope([])).toEqual({ projectKey: undefined, projectKeys: undefined });
  });
  it("compares drafts regardless of key order and blank titles", () => {
    const a = { name: "X ", description: "", shared: false, gadgets: [{ id: "1", type: "stat" as const, width: 1, config: { stat: "open" as const, days: 3 }, title: "" }] };
    const b = { name: "X", gadgets: [{ config: { days: 3, stat: "open" as const }, width: 1, type: "stat" as const, id: "1" }] };
    expect(sameDashboard(a, b)).toBe(true);
    expect(sameDashboard(a, { ...b, shared: true })).toBe(false);
  });
});
