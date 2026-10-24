import { describe, expect, it } from "vitest";
import {
  busy,
  currentStep,
  keyError,
  overallPercent,
  personOutcome,
  phasePercent,
  plural,
  reachable,
  reportRows,
  sanitizeKey,
  setPerson,
  setStatus,
  siteName,
  sortProblems,
  toggleProject,
  type JiraConfig,
  type JiraPerson,
} from "./model";

const cfg = (): JiraConfig => ({ projects: [], priorities: {}, storyPointsField: "", people: {}, componentsAsLabels: true, attachments: true });
const project = { id: "10000", key: "PLAT", name: "Platform", type: "software", teamManaged: false };

describe("jira import model", () => {
  it("derives the step and what can be opened", () => {
    const base = { status: "draft" as const, scanStale: true, config: cfg() };
    expect(currentStep(base)).toBe("projects");
    const chosen = { ...base, config: toggleProject(cfg(), project) };
    expect(currentStep(chosen)).toBe("mapping");
    expect(reachable(chosen, "dryrun")).toBe(false);
    expect(reachable({ ...chosen, scanStale: false }, "dryrun")).toBe(true);
    expect(reachable(chosen, "import")).toBe(false);
    expect(currentStep({ ...chosen, mode: "import" })).toBe("import");
    expect(busy({ status: "running", stalled: false })).toBe(true);
    expect(busy({ status: "running", stalled: true })).toBe(false);
  });

  it("selects projects with server-compatible default keys", () => {
    expect(sanitizeKey("PLAT")).toBe("PLAT");
    expect(sanitizeKey("ab_c")).toBe("ABC");
    expect(sanitizeKey("1x")).toBe("XX");
    expect(sanitizeKey("VERYLONGPROJECTKEY")).toBe("VERYLONGPR");
    expect(keyError("PLAT")).toBeUndefined();
    expect(keyError("pl")).toBeDefined();
    let c = toggleProject(cfg(), project);
    expect(c.projects).toEqual([{ jiraId: "10000", jiraKey: "PLAT", jiraName: "Platform", key: "PLAT", name: "Platform", template: "", open: true, statuses: {}, types: {} }]);
    c = setStatus(c, "10000", "3", "In Review");
    expect(c.projects[0].statuses).toEqual({ "3": "In Review" });
    c = toggleProject(c, project);
    expect(c.projects).toEqual([]);
  });

  it("explains what happens to each person", () => {
    const p: JiraPerson = { id: "a", displayName: "Ada", email: "ada@example.com", active: true, app: false, count: 3, placeholder: false };
    expect(personOutcome(p, undefined)).toEqual({ tone: "accent", text: "Placeholder; claimed when ada@example.com signs in" });
    expect(personOutcome({ ...p, match: { id: "u", displayName: "Ada L", email: "ada@example.com", active: true } }, undefined).text).toBe("Matched: Ada L");
    expect(personOutcome({ ...p, email: "" }, undefined).tone).toBe("warning");
    expect(personOutcome({ ...p, email: "" }, { action: "placeholder", email: "ada@corp.example" }).text).toContain("ada@corp.example");
    expect(personOutcome(p, { action: "skip" }).text).toBe("Left empty");
    let c = setPerson(cfg(), "a", { action: "skip" });
    expect(c.people).toEqual({ a: { action: "skip" } });
    c = setPerson(c, "a", { action: "auto" });
    expect(c.people).toEqual({});
  });

  it("summarises progress and reports", () => {
    expect(phasePercent({ key: "issues", label: "", status: "running", done: 25, total: 100 })).toBe(25);
    expect(phasePercent({ key: "issues", label: "", status: "done", done: 3, total: 100 })).toBe(100);
    expect(phasePercent({ key: "issues", label: "", status: "pending", done: 0, total: 0 })).toBe(0);
    expect(
      overallPercent({
        phases: [
          { key: "a", label: "", status: "done", done: 1, total: 1 },
          { key: "b", label: "", status: "pending", done: 0, total: 4 },
        ],
        counters: {},
        problems: [],
      }),
    ).toBe(50);
    expect(reportRows({ issuesCreated: 8, comments: 0, links: 3, unknown: 1 })).toEqual([
      { key: "issuesCreated", label: "Issues created", value: 8 },
      { key: "links", label: "Issue links", value: 3 },
    ]);
    const sorted = sortProblems([
      { severity: "info", kind: "x", message: "i", count: 1 },
      { severity: "error", kind: "x", message: "e", count: 1 },
      { severity: "warning", kind: "x", message: "w", count: 1 },
    ]);
    expect(sorted.map((p) => p.severity)).toEqual(["error", "warning", "info"]);
    expect(plural(1, "warning")).toBe("1 warning");
    expect(plural(3, "note")).toBe("3 notes");
    expect(siteName("https://acme.atlassian.net")).toBe("acme.atlassian.net");
    expect(siteName("https://jira.example.com/jira")).toBe("jira.example.com/jira");
  });
});
