import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import type { Issue } from "@/api/types";
import {
  applyBoard,
  assignStatus,
  boardInput,
  columnErrors,
  columnInputs,
  draftColumns,
  insertColumn,
  insertionIndex,
  joinNames,
  moveColumn,
  removeColumn,
  renameColumn,
  setWipLimit,
  withChanges,
  type ColumnDraft,
} from "./board-config";

type Board = Schemas["Board"];

const board: Board = {
  id: "b1",
  projectKey: "P",
  name: "Board",
  type: "kanban",
  filterJql: "",
  swimlanes: "none",
  doneRetentionDays: 14,
  columns: [
    { id: "c1", name: "To do", statusIds: ["todo"] },
    { id: "c2", name: "Doing", statusIds: ["doing", "review"], wipLimit: 2 },
    { id: "c3", name: "Done", statusIds: ["done"] },
  ],
  cardFields: ["epic", "points"],
};

const drafts = (): ColumnDraft[] => draftColumns(board.columns);

describe("drafts and the PUT body", () => {
  it("keys drafts by id and sends everything back", () => {
    expect(drafts().map((c) => c.key)).toEqual(["c1", "c2", "c3"]);
    expect(boardInput(board)).toEqual({
      name: "Board",
      type: "kanban",
      filterJql: "",
      swimlanes: "none",
      doneRetentionDays: 14,
      columns: [
        { id: "c1", name: "To do", statusIds: ["todo"] },
        { id: "c2", name: "Doing", statusIds: ["doing", "review"], wipLimit: 2 },
        { id: "c3", name: "Done", statusIds: ["done"] },
      ],
      cardFields: ["epic", "points"],
    });
  });
  it("never sends temporary ids, and trims names", () => {
    const { columns } = insertColumn(drafts(), 1, "  Ready ");
    const next = withChanges(board, { columns });
    expect(next.columns[1].id).toMatch(/^new-/);
    expect(boardInput(next).columns![1]).toEqual({ name: "Ready", statusIds: [] });
    expect(draftColumns(next.columns)[1].id).toBeUndefined();
    expect(columnInputs([{ key: "k", name: "x", statusIds: [], wipLimit: 0 }])).toEqual([{ name: "x", statusIds: [] }]);
  });
  it("defaults card fields when an older server leaves them out", () => {
    expect(boardInput({ ...board, cardFields: undefined as never }).cardFields).toEqual(["epic", "labels", "priority", "dev", "points", "assignee"]);
  });
});

describe("column edits", () => {
  it("renames, limits and moves", () => {
    expect(renameColumn(drafts(), "c1", "Inbox")[0].name).toBe("Inbox");
    expect(setWipLimit(drafts(), "c1", 3)[0].wipLimit).toBe(3);
    expect("wipLimit" in setWipLimit(drafts(), "c2", undefined)[1]).toBe(false);
    expect(moveColumn(drafts(), "c3", 0).map((c) => c.key)).toEqual(["c3", "c1", "c2"]);
    expect(moveColumn(drafts(), "c1", 99).map((c) => c.key)).toEqual(["c2", "c3", "c1"]);
    expect(moveColumn(drafts(), "zz", 0).map((c) => c.key)).toEqual(["c1", "c2", "c3"]);
  });
  it("inserts columns, taking statuses from others", () => {
    const { columns, key } = insertColumn(drafts(), 2, "Review", ["review"]);
    expect(columns.map((c) => c.name)).toEqual(["To do", "Doing", "Review", "Done"]);
    expect(columns[2]).toEqual({ key, name: "Review", statusIds: ["review"] });
    expect(columns[1].statusIds).toEqual(["doing"]);
  });
  it("removes columns and moves statuses", () => {
    expect(removeColumn(drafts(), "c2").flatMap((c) => c.statusIds)).toEqual(["todo", "done"]);
    const moved = assignStatus(drafts(), "review", "c3");
    expect(moved[1].statusIds).toEqual(["doing"]);
    expect(moved[2].statusIds).toEqual(["done", "review"]);
    expect(assignStatus(drafts(), "todo", null)[0].statusIds).toEqual([]);
    expect(assignStatus(drafts(), "new", "c1")[0].statusIds).toEqual(["todo", "new"]);
  });
  it("suggests where a new column for a status goes", () => {
    const cat = (s: string) => (({ todo: "todo", doing: "in_progress", review: "in_progress", done: "done" }) as const)[s as "todo"];
    expect(insertionIndex(drafts(), "todo", cat)).toBe(1);
    expect(insertionIndex(drafts(), "in_progress", cat)).toBe(2);
    expect(insertionIndex(drafts(), "done", cat)).toBe(3);
    expect(insertionIndex([], "done", cat)).toBe(0);
  });
});

const issue = (id: string, status: string, rank: string): Issue => ({
  id, key: `P-${id}`, projectKey: "P", type: { id: "t", key: "task", name: "Task", icon: "task", color: "#000", level: 0 },
  summary: id, status: { id: status, name: status, category: "todo" }, priority: "medium", labels: [], rank, version: 1, fields: {}, createdAt: "", updatedAt: "",
});

describe("applyBoard", () => {
  const data: Schemas["BoardData"] = {
    board,
    unmappedStatusIds: ["qa"],
    columns: [
      { id: "c1", name: "To do", statusIds: ["todo"], overLimit: false, issues: [issue("1", "todo", "b"), issue("2", "todo", "d")] },
      { id: "c2", name: "Doing", statusIds: ["doing", "review"], wipLimit: 2, overLimit: false, issues: [issue("3", "doing", "a"), issue("4", "review", "c")] },
      { id: "c3", name: "Done", statusIds: ["done"], overLimit: false, issues: [] },
    ],
  };

  it("regroups issues by status, re-sorting merged columns by rank", () => {
    const next = withChanges(board, { columns: [{ key: "c1", id: "c1", name: "Open", statusIds: ["todo", "doing"], wipLimit: 2 }, { key: "c3", id: "c3", name: "Done", statusIds: ["done", "qa"] }] });
    const out = applyBoard(data, next);
    expect(out.board).toBe(next);
    expect(out.columns.map((c) => [c.id, c.name, c.issues.map((i) => i.id), c.overLimit])).toEqual([
      ["c1", "Open", ["3", "1", "2"], true],
      ["c3", "Done", [], false],
    ]);
    // review left the board; qa is mapped now.
    expect(out.unmappedStatusIds).toEqual(["review"]);
  });

  it("keeps the order within a column and gives new columns their temporary id", () => {
    const { columns, key } = insertColumn(drafts(), 1, "Review", ["review"]);
    const out = applyBoard(data, withChanges(board, { columns }));
    expect(out.columns.map((c) => c.id)).toEqual(["c1", key, "c2", "c3"]);
    expect(out.columns[0].issues.map((i) => i.id)).toEqual(["1", "2"]);
    expect(out.columns[1].issues.map((i) => i.id)).toEqual(["4"]);
    expect(out.unmappedStatusIds).toEqual(["qa"]);
  });
});

describe("columnErrors", () => {
  it("routes field paths to columns", () => {
    expect(columnErrors({ columns: "a status can only be in one column", name: "x", "columns[1].name": "required", "columns.2": "bad", "columns[1].wipLimit": "later" })).toEqual({
      general: "a status can only be in one column",
      byIndex: { 1: "required", 2: "bad" },
    });
  });
});

describe("joinNames", () => {
  it("joins with commas and 'and'", () => {
    expect(joinNames([])).toBe("");
    expect(joinNames(["A"])).toBe("A");
    expect(joinNames(["A", "B"])).toBe("A and B");
    expect(joinNames(["A", "B", "C"])).toBe("A, B and C");
  });
});
