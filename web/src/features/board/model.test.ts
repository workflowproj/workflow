import { describe, expect, it } from "vitest";
import type { Issue } from "@/api/types";
import { buildLanes, cellId, findContainer, insertWithHint, moveAcross, neighbours, parseCellId, placeInBacklog, placeOnBoard, planDrop, sumPoints } from "./model";

const type = (level: number, name = level === 1 ? "Epic" : level === -1 ? "Sub-task" : "Story") => ({ id: `t${level}`, key: name.toLowerCase(), name, icon: "story", color: "#000", level, kind: "work" as const });
const status = (category: "todo" | "in_progress" | "done" = "todo") => ({ id: `s-${category}`, name: category, category });

function issue(id: string, over: Partial<Issue> = {}): Issue {
  return {
    id, key: `P-${id}`, projectKey: "P", type: type(0), summary: id, status: status(), priority: "medium",
    labels: [], rank: id, version: 1, fields: {}, createdAt: "", updatedAt: "", ...over,
  };
}

const col = (id: string, issues: Issue[], wipLimit?: number) => ({ id, name: id, statusIds: [`st-${id}`], issues, wipLimit, overLimit: false });

describe("neighbours", () => {
  it("prefers the issue above, then the one below", () => {
    expect(neighbours(["a", "b", "c"], "b")).toEqual({ afterIssueId: "a" });
    expect(neighbours(["a", "b", "c"], "a")).toEqual({ beforeIssueId: "b" });
    expect(neighbours(["a"], "a")).toEqual({});
    expect(neighbours(["a"], "x")).toEqual({});
  });
});

describe("containers", () => {
  it("finds and moves items", () => {
    const c = { x: ["1", "2"], y: ["3"] };
    expect(findContainer(c, "3")).toBe("y");
    expect(findContainer(c, "x")).toBe("x");
    expect(findContainer(c, "9")).toBeUndefined();
    expect(moveAcross(c, "1", "x", "y", 0)).toEqual({ x: ["2"], y: ["1", "3"] });
    expect(moveAcross(c, "1", "x", "y", 99)).toEqual({ x: ["2"], y: ["3", "1"] });
  });
  it("round-trips cell ids containing colons", () => {
    expect(parseCellId(cellId("assignee:abc", "col-1"))).toEqual({ laneId: "assignee:abc", columnId: "col-1" });
  });
});

describe("insertWithHint", () => {
  const list = [issue("b"), issue("d")];
  it("places after or before a neighbour", () => {
    expect(insertWithHint(list, issue("x"), { afterIssueId: "b" }).map((i) => i.id)).toEqual(["b", "x", "d"]);
    expect(insertWithHint(list, issue("x"), { beforeIssueId: "b" }).map((i) => i.id)).toEqual(["x", "b", "d"]);
  });
  it("falls back to rank order", () => {
    expect(insertWithHint(list, issue("c"), {}).map((i) => i.id)).toEqual(["b", "c", "d"]);
    expect(insertWithHint(list, issue("z"), { afterIssueId: "missing" }).map((i) => i.id)).toEqual(["b", "d", "z"]);
  });
});

describe("placeOnBoard", () => {
  it("moves the issue, updates its status and the WIP flag", () => {
    const data = { columns: [col("todo", [issue("a"), issue("b")]), col("doing", [issue("c")], 1)] };
    const out = placeOnBoard(data, "a", "doing", { beforeIssueId: "c" }, status("in_progress"));
    expect(out.columns[0].issues.map((i) => i.id)).toEqual(["b"]);
    expect(out.columns[1].issues.map((i) => i.id)).toEqual(["a", "c"]);
    expect(out.columns[1].issues[0].status.category).toBe("in_progress");
    expect(out.columns[1].overLimit).toBe(true);
  });
});

describe("planDrop", () => {
  const to = (id: string, required: boolean[] = []) => ({
    id: `t-${id}`,
    name: id,
    to: { id, name: id, category: "done" as const },
    screen: required.map((r, i) => ({ field: `f${i}`, name: `f${i}`, type: "text", required: r })),
  });

  it("re-ranks within the status and lets the API pick when transitions aren't loaded", () => {
    expect(planDrop("a", ["a", "b"], [to("b")])).toEqual({ kind: "rank" });
    expect(planDrop("a", ["b"], undefined)).toEqual({ kind: "move", transition: null });
  });

  it("moves at once when nothing needs confirming", () => {
    expect(planDrop("a", ["b"], [to("b", [false]), to("c")])).toEqual({ kind: "move", transition: to("b", [false]) });
  });

  it("asks first when the column has several targets or the transition has a required screen", () => {
    expect(planDrop("a", ["b", "c"], [to("b"), to("c")])).toEqual({ kind: "choose", options: [to("b"), to("c")] });
    expect(planDrop("a", ["b"], [to("b", [false, true])])).toEqual({ kind: "screen", transition: to("b", [false, true]) });
  });

  it("refuses when no transition reaches the column", () => {
    expect(planDrop("a", ["b"], [to("c")])).toEqual({ kind: "refuse" });
  });
});

describe("placeInBacklog", () => {
  it("moves between a sprint and the backlog", () => {
    const data = { sprints: [{ sprint: { id: "s1" }, issues: [issue("a", { sprintId: "s1" })] }], backlog: [issue("b")] };
    const out = placeInBacklog(data, "a", "backlog", { afterIssueId: "b" });
    expect(out.sprints[0].issues).toEqual([]);
    expect(out.backlog.map((i) => i.id)).toEqual(["b", "a"]);
    expect(out.backlog[1].sprintId).toBeUndefined();
    const back = placeInBacklog(out, "a", "s1", {});
    expect(back.sprints[0].issues[0].sprintId).toBe("s1");
  });
});

describe("buildLanes", () => {
  const ada = { id: "u1", displayName: "Ada", email: "", kind: "human" as const };
  const bob = { id: "u2", displayName: "Bob", email: "", kind: "human" as const };
  const epic = { id: "e1", key: "P-1", summary: "Checkout", type: type(1), status: status() };
  const story = issue("s", { assignee: bob, parent: epic });
  const sub = issue("t", { type: type(-1), parent: { id: "s", key: "P-s", summary: "s", type: type(0), status: status() } });
  const cols = [col("todo", [story, sub]), col("done", [issue("u", { assignee: ada, priority: "high" })])];

  it("gives one lane, even when empty, without swimlanes", () => {
    expect(buildLanes([col("todo", [])], "none")).toHaveLength(1);
  });
  it("groups by assignee with me first and unassigned last", () => {
    const lanes = buildLanes(cols, "assignee", "u1");
    expect(lanes.map((l) => l.label)).toEqual(["Ada", "Bob", "Unassigned"]);
    expect(lanes[1].cells.todo.map((i) => i.id)).toEqual(["s"]);
  });
  it("puts sub-tasks in their parent's epic", () => {
    const lanes = buildLanes(cols, "epic");
    expect(lanes.map((l) => l.label)).toEqual(["Checkout", "No epic"]);
    expect(lanes[0].cells.todo.map((i) => i.id)).toEqual(["s", "t"]);
  });
  it("orders priorities from highest", () => {
    expect(buildLanes(cols, "priority").map((l) => l.label)).toEqual(["High", "Medium"]);
  });
});

describe("sumPoints", () => {
  it("splits by category", () => {
    const s = sumPoints([issue("a", { storyPoints: 3 }), issue("b", { storyPoints: 2, status: status("done") }), issue("c")]);
    expect(s).toEqual({ total: 5, todo: 3, in_progress: 0, done: 2, estimated: 2 });
  });
});
