import { describe, expect, it } from "vitest";
import { projectHome } from "./project";

describe("projectHome", () => {
  it("opens service projects on their queues and others on their board", () => {
    expect(projectHome({ template: "service" })).toBe("/projects/$projectKey/queues");
    for (const template of ["scrum", "kanban", "bug_tracking", "risks_decisions"]) expect(projectHome({ template })).toBe("/projects/$projectKey/board");
  });
});
