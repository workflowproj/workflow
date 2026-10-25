import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { measureOf, sprintMeasure } from "./BurndownChart";

const H = 3600;
const point = (p: Partial<Schemas["BurndownPoint"]>): Schemas["BurndownPoint"] => ({
  date: "2026-10-20",
  remainingPoints: 0,
  idealPoints: 0,
  scopePoints: 0,
  completedPoints: 0,
  remainingIssues: 0,
  scopeIssues: 0,
  completedIssues: 0,
  remainingSeconds: 0,
  idealSeconds: 0,
  scopeSeconds: 0,
  completedSeconds: 0,
  ...p,
});
const report: Schemas["SprintReport"] = {
  sprint: { id: "s", name: "Sprint 1", goal: "", state: "active", position: 0, startDate: "2026-10-20", endDate: "2026-10-30" },
  committedPoints: 8,
  completedPoints: 3,
  committedIssues: 2,
  completedIssues: 1,
  addedPoints: 0,
  removedPoints: 0,
  addedIssues: 0,
  removedIssues: 0,
  committedSeconds: 16 * H,
  completedSeconds: 4.5 * H,
  addedSeconds: 2 * H,
  removedSeconds: 0,
  burndown: [point({ remainingSeconds: 12 * H, scopeSeconds: 16.5 * H, completedSeconds: 4.5 * H, scopePoints: 8 })],
};

describe("sprint measures", () => {
  it("defaults to points, or issues for an unestimated sprint; hours only when chosen", () => {
    expect(sprintMeasure(report)).toBe("points");
    expect(sprintMeasure({ ...report, committedPoints: 0, burndown: [point({})] })).toBe("issues");
    expect(sprintMeasure(report, "hours")).toBe("hours");
  });
  it("reads hours from the seconds fields", () => {
    const m = measureOf(report, "hours");
    expect([m.committed, m.completed, m.added, m.removed]).toEqual([16, 4.5, 2, 0]);
    const p = report.burndown[0];
    expect([m.remaining(p), m.scope(p), m.done(p)]).toEqual([12, 16.5, 4.5]);
    expect(m.unit).toBe("hours");
    expect(m.doneLabel).toBe("Logged");
    expect(m.format(12.345)).toBe("12.3");
  });
  it("keeps points and issues as they were", () => {
    expect(measureOf(report, "points")).toMatchObject({ committed: 8, completed: 3, unit: "pts", doneLabel: "Completed" });
    expect(measureOf(report, "issues")).toMatchObject({ committed: 2, completed: 1, unit: "issues" });
  });
});
