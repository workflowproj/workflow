import { describe, expect, it } from "vitest";
import {
  addStatus,
  addTransition,
  blankWorkflow,
  deleteStatus,
  diffDocs,
  docEqual,
  fromDefinition,
  normalize,
  parseWorkflowYaml,
  removeEdge,
  slug,
  targetOfPath,
  tidyLayout,
  updateStatus,
  updateTransition,
  withLayout,
  workflowToYaml,
  type WorkflowDoc,
} from "./model";

const bugTriage = `
kind: Workflow
name: Bug triage
description: Open → Closed
statuses:
  - { name: Open, category: todo }
  - { name: In Progress, category: in_progress }
  - { name: Closed, category: done }
  - { name: Won't Fix, category: done }
transitions:
  - { name: Start work, from: Open, to: In Progress, postFunctions: [{ assignTo: actor }] }
  - name: Close
    from: [Open, In Progress]
    to: Closed
    screen: [{ field: resolution, required: true, options: [Fixed, Not a bug] }, comment]
    validators:
      - 'issue.subtasks.all(s, s.category == "done")'
      - { expr: 'issue.assignee != ""', message: Assign it first. }
  - name: Won't fix
    to: Won't Fix
    condition: 'actor.hasRole("admin")'
  - { name: Reopen, from: ["*"], to: Open, postFunctions: [{ setField: { field: priority, value: high } }, { notify: watchers }] }
layout:
  Open: { x: 0, y: 120 }
  Closed: { x: 480.4, y: 60 }
`;

describe("parseWorkflowYaml", () => {
  it("accepts the server's short forms", () => {
    const r = parseWorkflowYaml(bugTriage);
    if (!r.ok) throw new Error(r.error);
    const d = r.doc;
    expect(d.name).toBe("Bug triage");
    expect(d.statuses.map((s) => s.name)).toEqual(["Open", "In Progress", "Closed", "Won't Fix"]);
    expect(d.transitions[0].from).toEqual(["Open"]); // `from: Open` is a one-element list
    expect(d.transitions[1].screen).toEqual([{ field: "resolution", required: true, options: ["Fixed", "Not a bug"] }, { field: "comment" }]);
    expect(d.transitions[1].validators).toEqual([{ expr: 'issue.subtasks.all(s, s.category == "done")' }, { expr: 'issue.assignee != ""', message: "Assign it first." }]);
    expect(d.transitions[2].from).toBeUndefined(); // global
    expect(d.transitions[3].from).toBeUndefined(); // "*" means any
    expect(d.transitions[3].postFunctions).toEqual([{ setField: { field: "priority", value: "high" } }, { notify: "watchers" }]);
  });

  it("keeps YAML 1.2 scalars such as `on` and `y` as strings", () => {
    const r = parseWorkflowYaml("name: x\nstatuses:\n  - { name: on, category: todo }\n  - { name: y, category: done }\ntransitions: []\n");
    expect(r.ok && r.doc.statuses.map((s) => s.name)).toEqual(["on", "y"]);
  });

  it("reports syntax errors with a line", () => {
    const r = parseWorkflowYaml("name: x\nstatuses:\n  - { name: Open\n");
    expect(r.ok).toBe(false);
    expect(!r.ok && r.line).toBeGreaterThan(0);
  });

  it("rejects documents that are not mappings", () => {
    expect(parseWorkflowYaml("").ok).toBe(false);
    expect(parseWorkflowYaml("- a\n- b").ok).toBe(false);
    expect(parseWorkflowYaml("name: x\nstatuses: nope").ok).toBe(false);
  });
});

describe("workflowToYaml", () => {
  it("round-trips through YAML", () => {
    const r = parseWorkflowYaml(bugTriage);
    if (!r.ok) throw new Error(r.error);
    const yaml = workflowToYaml(r.doc);
    const back = parseWorkflowYaml(yaml);
    if (!back.ok) throw new Error(back.error);
    expect(docEqual(back.doc, r.doc)).toBe(true);
    expect(back.doc.transitions[1].validators?.[0].expr).toBe('issue.subtasks.all(s, s.category == "done")');
  });

  it("writes readable block YAML in the server's key order", () => {
    const yaml = workflowToYaml(blankWorkflow("Delivery"));
    expect(yaml.startsWith("kind: Workflow\nname: Delivery\n")).toBe(true);
    expect(yaml).toContain("  - {name: To Do, category: todo, initial: true}");
    expect(yaml).toContain("    from: [To Do]");
    expect(yaml).toContain("layout:\n  To Do: {x: 0, y: 120}");
    expect(yaml.indexOf("statuses:")).toBeLessThan(yaml.indexOf("transitions:"));
  });

  it("omits empty optional parts and rounds positions", () => {
    const doc: WorkflowDoc = {
      name: "W",
      statuses: [{ name: "A", category: "todo" }],
      transitions: [{ name: "Any", to: "A", condition: " ", validators: [], screen: [], postFunctions: [] }],
      layout: { A: { x: 10.6, y: -3.2 }, Gone: { x: 1, y: 1 } },
    };
    const yaml = workflowToYaml(doc);
    expect(yaml).not.toContain("condition");
    expect(yaml).not.toContain("validators");
    expect(yaml).not.toContain("Gone"); // layout entries for unknown statuses are dropped
    expect(yaml).toContain("A: {x: 11, y: -3}");
  });

  it("quotes values that would otherwise change type", () => {
    const doc: WorkflowDoc = {
      name: "W",
      statuses: [
        { name: "123", category: "todo" },
        { name: "true", category: "done" },
      ],
      transitions: [],
    };
    const back = parseWorkflowYaml(workflowToYaml(doc));
    expect(back.ok && back.doc.statuses.map((s) => s.name)).toEqual(["123", "true"]);
  });
});

describe("fromDefinition", () => {
  it("resolves status ids back to names", () => {
    const doc = fromDefinition({
      name: "W",
      statuses: [
        { id: "a", name: "Open", category: "todo", initial: true },
        { id: "b", name: "Done", category: "done", initial: false },
      ],
      transitions: [
        { id: "close", name: "Close", from: ["a"], to: "b" },
        { id: "reset", name: "Reset", to: "a" },
      ],
    });
    expect(doc.transitions[0]).toEqual({ id: "close", name: "Close", from: ["Open"], to: "Done" });
    expect(doc.transitions[1].from).toBeUndefined();
    expect(doc.statuses[0].initial).toBe(true);
    expect(doc.statuses[1].initial).toBeUndefined();
  });
});

describe("editing operations", () => {
  const base = withLayout(blankWorkflow("W"));

  it("renaming a status updates transitions and layout", () => {
    const d = updateStatus(base, "In Progress", { name: "Doing" });
    expect(d.statuses[1].name).toBe("Doing");
    expect(d.transitions.find((t) => t.name === "Start work")?.to).toBe("Doing");
    expect(d.transitions.find((t) => t.name === "Done")?.from).toEqual(["Doing"]);
    expect(d.layout?.Doing).toEqual(base.layout?.["In Progress"]);
    expect(d.layout?.["In Progress"]).toBeUndefined();
  });

  it("can rename transitions that were named after a new status", () => {
    const a = addStatus(base);
    const t = addTransition(a.doc, "To Do", a.name);
    const d = updateStatus(t.doc, a.name, { name: "Blocked" }, { renameAutoNamed: true });
    expect(d.transitions[t.index]).toEqual({ name: "Blocked", from: ["To Do"], to: "Blocked" });
    // Existing transitions keep their names (and ids) unless asked.
    expect(updateStatus(base, "Done", { name: "Closed" }).transitions.find((x) => x.to === "Closed")?.name).toBe("Done");
  });

  it("marking a status initial clears the others", () => {
    const d = updateStatus(base, "Done", { initial: true });
    expect(d.statuses.filter((s) => s.initial).map((s) => s.name)).toEqual(["Done"]);
    expect(d.statuses[0]).toEqual({ name: "To Do", category: "todo" });
  });

  it("deleting a status removes its transitions without creating global ones", () => {
    const d = deleteStatus(base, "In Progress");
    expect(d.statuses.map((s) => s.name)).toEqual(["To Do", "Done"]);
    expect(d.transitions.map((t) => t.name)).toEqual(["Reopen"]);
    expect(d.layout?.["In Progress"]).toBeUndefined();
  });

  it("adds statuses with unique names and positions", () => {
    const a = addStatus(base, { position: { x: 5, y: 6 } });
    expect(a.name).toBe("New status");
    expect(a.doc.layout?.["New status"]).toEqual({ x: 5, y: 6 });
    const b = addStatus(a.doc);
    expect(b.name).toBe("New status 2");
    expect(b.doc.layout?.["New status 2"]).toBeDefined();
  });

  it("adds transitions named after their target, uniquely", () => {
    const a = addTransition(base, "To Do", "Done");
    expect(a.doc.transitions[a.index]).toEqual({ name: "Done (2)", from: ["To Do"], to: "Done" });
    const g = addTransition(base, undefined, "To Do", "Reset");
    expect(g.doc.transitions[g.index].from).toBeUndefined();
  });

  it("removing one edge of a multi-source transition keeps the others", () => {
    let d = updateTransition(base, 2, { from: ["To Do", "In Progress"] });
    d = removeEdge(d, 2, "To Do");
    expect(d.transitions[2].from).toEqual(["In Progress"]);
    d = removeEdge(d, 2, "In Progress");
    expect(d.transitions.map((t) => t.name)).toEqual(["Start work", "Stop work", "Reopen"]);
  });

  it("updateTransition drops emptied optional fields", () => {
    const d = updateTransition(base, 0, { condition: "", validators: [], id: undefined });
    expect(Object.keys(d.transitions[0]).sort()).toEqual(["from", "name", "to"]);
  });

  it("slugs like the server", () => {
    expect(slug("Submit for review")).toBe("submit-for-review");
    expect(slug("  Won't fix! ")).toBe("won-t-fix");
  });
});

describe("layout", () => {
  it("tidies statuses into columns by distance from the initial status", () => {
    const pos = tidyLayout(blankWorkflow("W"));
    expect(pos["To Do"].x).toBeLessThan(pos["In Progress"].x);
    expect(pos["In Progress"].x).toBeLessThan(pos["Done"].x);
  });

  it("places statuses missing from the layout below the others", () => {
    const d = withLayout({ ...blankWorkflow("W"), statuses: [...blankWorkflow("W").statuses, { name: "Blocked", category: "in_progress" }] });
    expect(d.layout?.Blocked.y).toBeGreaterThan(d.layout!["To Do"].y);
  });
});

describe("normalize and paths", () => {
  it("maps validation paths to elements", () => {
    expect(targetOfPath("statuses[2].category")).toEqual({ kind: "status", index: 2 });
    expect(targetOfPath("transitions[0].from[1]")).toEqual({ kind: "transition", index: 0 });
    expect(targetOfPath("layout")).toEqual({ kind: "workflow" });
    expect(targetOfPath(undefined)).toEqual({ kind: "workflow" });
  });

  it("treats an empty from list as from any", () => {
    expect(normalize({ name: "W", statuses: [], transitions: [{ name: "x", to: "A", from: [] }] }).transitions[0].from).toBeUndefined();
  });
});

describe("diffDocs", () => {
  it("summarises status and transition changes", () => {
    const before = withLayout(blankWorkflow("W"));
    let after = updateStatus(before, "In Progress", { name: "Doing" });
    after = updateTransition(after, 0, { condition: "true" });
    after = addTransition(after, "To Do", "Done", "Skip").doc;
    const d = diffDocs(before, after);
    expect(d.addedStatuses).toEqual(["Doing"]);
    expect(d.removedStatuses).toEqual(["In Progress"]);
    expect(d.addedTransitions).toEqual(["Skip"]);
    expect(d.changedTransitions).toContain("Start work");
    expect(d.layoutChanged).toBe(true); // the layout key moved with the rename
    expect(diffDocs(before, before)).toMatchObject({ addedStatuses: [], changedTransitions: [], layoutChanged: false });
  });
});
