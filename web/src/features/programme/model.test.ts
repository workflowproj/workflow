import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { crossTeamLinks, dependencyMatrix, donePercent, forecastLate, groupByTeam, progressText, roadmapTotals } from "./model";

const status = (category: "todo" | "in_progress" | "done") => ({ id: category, name: category, category, color: "" });
const issue = (key: string, category: "todo" | "in_progress" | "done" = "todo") =>
  ({ id: key, key, projectKey: key.split("-")[0], summary: key, status: status(category), type: { id: "t", key: "epic", name: "Epic", icon: "epic", color: "", level: 1 } }) as unknown as Schemas["Issue"];
const progress = (done: number, total: number, donePoints = 0, points = 0) => ({ done, total, inProgress: 0, donePoints, points });
const epic = (key: string, flags: Schemas["PlanningFlag"]["kind"][] = []) =>
  ({ issue: issue(key), progress: progress(0, 0), flags: flags.map((kind) => ({ kind, message: kind, issueKeys: [], hiddenCount: 0 })) }) as Schemas["ProgrammeEpic"];

describe("groupByTeam", () => {
  it("keeps the order teams first appear in", () => {
    const groups = groupByTeam([epic("API-1"), epic("APP-2"), epic("API-3")]);
    expect(groups.map((g) => [g.projectKey, g.epics.map((e) => e.issue.key)])).toEqual([
      ["API", ["API-1", "API-3"]],
      ["APP", ["APP-2"]],
    ]);
  });
});

describe("progress", () => {
  it("reads issues and points", () => {
    expect(progressText(progress(0, 0))).toBe("No issues yet");
    expect(progressText(progress(3, 8))).toBe("3 of 8 done");
    expect(progressText(progress(3, 8, 5, 21.5))).toBe("3 of 8 done · 5 of 21.5 pts");
    expect(donePercent(progress(1, 3))).toBe(33);
    expect(donePercent(progress(0, 0))).toBe(0);
  });

  it("flags a forecast after the due date", () => {
    expect(forecastLate("2026-12-01", "2026-12-02")).toBe(true);
    expect(forecastLate("2026-12-01", "2026-12-01")).toBe(false);
    expect(forecastLate(undefined, "2026-12-02")).toBe(false);
  });
});

describe("roadmapTotals", () => {
  it("counts initiatives, epics and flagged epics", () => {
    const o = {
      initiatives: [
        { issue: issue("PROG-1", "in_progress"), progress: progress(0, 0), flags: [], epics: [epic("APP-1", ["late", "blocked"]), epic("API-1")] },
        { issue: issue("PROG-2", "done"), progress: progress(0, 0), flags: [], epics: [epic("APP-2", ["blocked"])] },
      ],
    } as unknown as Schemas["ProgrammeOverview"];
    expect(roadmapTotals(o)).toEqual({ initiatives: 2, open: 1, epics: 3, late: 1, blocked: 2 });
  });
});

describe("dependencies between teams", () => {
  const issues = [issue("APP-1"), issue("APP-2"), issue("API-1"), issue("OPS-1")];
  const link = (sourceKey: string, targetKey: string) => ({ id: `${sourceKey}>${targetKey}`, sourceKey, targetKey, type: "blocks" as const });
  it("keeps links across projects and counts them by blocking and blocked team", () => {
    const links = crossTeamLinks({ issues, links: [link("APP-1", "APP-2"), link("API-1", "APP-1"), link("API-1", "APP-2"), link("OPS-1", "API-1"), link("APP-1", "MISSING-1")] });
    expect(links.map((l) => l.link.id)).toEqual(["API-1>APP-1", "API-1>APP-2", "OPS-1>API-1"]);
    const m = dependencyMatrix(links);
    expect(m.get("API")?.get("APP")?.length).toBe(2);
    expect(m.get("OPS")?.get("API")?.length).toBe(1);
    expect(m.get("APP")).toBeUndefined();
  });
});
