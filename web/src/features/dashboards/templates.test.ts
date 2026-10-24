import { describe, expect, it } from "vitest";
import { cleanConfig, gadgetMeta } from "./model";
import { TEMPLATES, templateById } from "./templates";

describe("templates", () => {
  for (const t of TEMPLATES) {
    describe(t.name, () => {
      const d = t.build(t.projects === "none" ? [] : ["PLAT"]);
      it("builds valid gadgets", () => {
        expect(d.name.length).toBeGreaterThan(0);
        expect(d.name.length).toBeLessThanOrEqual(80);
        expect(d.gadgets.length).toBeLessThanOrEqual(40);
        expect(new Set(d.gadgets.map((g) => g.id)).size).toBe(d.gadgets.length);
        for (const g of d.gadgets) {
          expect(g.id.length).toBeLessThanOrEqual(40);
          expect([1, 2, 3]).toContain(g.width);
          // Templates only set what each type uses.
          expect(cleanConfig(g.type, g.config)).toEqual(g.config);
          if (gadgetMeta(g.type).scope !== "query") expect(g.config.projectKey).toBe("PLAT");
        }
      });
      it("fills whole rows of the three-column grid", () => {
        expect(d.gadgets.reduce((n, g) => n + g.width, 0) % 3).toBe(0);
      });
    });
  }

  it("scopes the sprint dashboard to the project and its active sprint", () => {
    const d = templateById("sprint").build(["PLAT"]);
    expect(d.name).toBe("Sprint dashboard · PLAT");
    expect(d.gadgets.map((g) => g.type)).toEqual(["sprint_health", "sprint_burndown", "sprint_burnup", "velocity", "breakdown", "issue_list"]);
    const list = d.gadgets.find((g) => g.type === "issue_list")!;
    expect(list.config.projectKeys).toEqual(["PLAT"]);
    expect(list.config.jql).toMatch(/^sprint in openSprints\(\) AND statusCategory != Done AND \(due < startOfDay\(\) OR issue in blockedIssues\(\)\) ORDER BY/);
  });

  it("covers every project when team flow gets none", () => {
    const all = templateById("flow").build([]);
    expect(all.gadgets.every((g) => g.config.projectKeys === undefined)).toBe(true);
    expect(templateById("flow").build(["A", "B"]).gadgets[0].config.projectKeys).toEqual(["A", "B"]);
  });

  it("filters my work to the viewer", () => {
    const d = templateById("mine").build([]);
    expect(d.gadgets.every((g) => g.config.jql?.startsWith("assignee = currentUser()"))).toBe(true);
    expect(d.shared).toBe(false);
  });

  it("falls back to blank", () => {
    expect(templateById("blank").build([]).gadgets).toEqual([]);
  });
});
