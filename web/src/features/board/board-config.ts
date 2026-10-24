import type { Schemas } from "@/api/client";
import type { StatusCategory } from "@/api/types";
import { normalizeCardFields, type CardField } from "./card-fields";

/*
 * Pure helpers behind board configuration, shared by the board's own editing (column menus,
 * the configure drawer) and the settings page: column drafts, the PUT /boards/{id} body and
 * the optimistic board after a change.
 */

type Board = Schemas["Board"];
type BoardData = Schemas["BoardData"];
type BoardColumn = Schemas["BoardColumn"];

/** A column being edited. `key` is stable while editing; `id` is the server's, when it has one. */
export type ColumnDraft = { key: string; id?: string; name: string; statusIds: string[]; wipLimit?: number };

const TEMP = "new-";
let seq = 0;
/** newColumnKey names a column the server hasn't seen yet. */
export const newColumnKey = () => `${TEMP}${++seq}`;
const isTemp = (id: string | undefined) => !id || id.startsWith(TEMP);

/** draftColumns turns a board's columns into drafts keyed by their ids. */
export function draftColumns(columns: BoardColumn[]): ColumnDraft[] {
  return columns.map((c) => {
    const id = isTemp(c.id) ? undefined : c.id;
    return { key: c.id ?? newColumnKey(), ...(id ? { id } : {}), name: c.name, statusIds: [...c.statusIds], ...(c.wipLimit ? { wipLimit: c.wipLimit } : {}) };
  });
}

/** columnInputs is the API form of drafts: names trimmed, no WIP limit when unset, server ids only. */
export function columnInputs(drafts: ColumnDraft[]): BoardColumn[] {
  return drafts.map((c) => ({ ...(c.id ? { id: c.id } : {}), name: c.name.trim(), statusIds: c.statusIds, ...(c.wipLimit ? { wipLimit: c.wipLimit } : {}) }));
}

/** boardInput is the full PUT body for a board; columns are always sent so they aren't re-derived. */
export function boardInput(board: Board): Schemas["BoardInput"] {
  return {
    name: board.name,
    type: board.type,
    filterJql: board.filterJql,
    swimlanes: board.swimlanes,
    doneRetentionDays: board.doneRetentionDays,
    columns: columnInputs(draftColumns(board.columns)),
    cardFields: normalizeCardFields(board.cardFields),
  };
}

/** withChanges is the board as it will be after a save: drafts become columns (new ones keep their temporary key as id). */
export function withChanges(board: Board, change: { columns?: ColumnDraft[]; cardFields?: CardField[] }): Board {
  return {
    ...board,
    columns: change.columns
      ? change.columns.map((c) => ({ id: c.id ?? c.key, name: c.name.trim(), statusIds: c.statusIds, ...(c.wipLimit ? { wipLimit: c.wipLimit } : {}) }))
      : board.columns,
    cardFields: change.cardFields ?? normalizeCardFields(board.cardFields),
  };
}

// ---------------------------------------------------------------------------
// Column edits. Each returns a new array; unknown keys leave the columns unchanged.

export function renameColumn(cols: ColumnDraft[], key: string, name: string): ColumnDraft[] {
  return cols.map((c) => (c.key === key ? { ...c, name } : c));
}

export function setWipLimit(cols: ColumnDraft[], key: string, limit: number | undefined): ColumnDraft[] {
  return cols.map((c) => {
    if (c.key !== key) return c;
    const { wipLimit: _old, ...rest } = c;
    return limit && limit > 0 ? { ...rest, wipLimit: Math.floor(limit) } : rest;
  });
}

/** moveColumn moves a column to index `to` (clamped). */
export function moveColumn(cols: ColumnDraft[], key: string, to: number): ColumnDraft[] {
  const from = cols.findIndex((c) => c.key === key);
  if (from < 0) return cols;
  const out = cols.filter((c) => c.key !== key);
  out.splice(Math.max(0, Math.min(to, out.length)), 0, cols[from]);
  return out;
}

/** insertColumn adds an empty (or pre-filled) column at `index`; any statuses given leave their old columns. */
export function insertColumn(cols: ColumnDraft[], index: number, name: string, statusIds: string[] = []): { columns: ColumnDraft[]; key: string } {
  const key = newColumnKey();
  const taken = new Set(statusIds);
  const out = cols.map((c) => (c.statusIds.some((s) => taken.has(s)) ? { ...c, statusIds: c.statusIds.filter((s) => !taken.has(s)) } : c));
  out.splice(Math.max(0, Math.min(index, out.length)), 0, { key, name, statusIds: [...statusIds] });
  return { columns: out, key };
}

/** removeColumn deletes a column; its statuses become unmapped. */
export function removeColumn(cols: ColumnDraft[], key: string): ColumnDraft[] {
  return cols.filter((c) => c.key !== key);
}

/** assignStatus moves a status to the end of a column, or off the board when `to` is null. */
export function assignStatus(cols: ColumnDraft[], statusId: string, to: string | null): ColumnDraft[] {
  return cols.map((c) => {
    const without = c.statusIds.filter((s) => s !== statusId);
    if (c.key === to) return { ...c, statusIds: [...without, statusId] };
    return without.length === c.statusIds.length ? c : { ...c, statusIds: without };
  });
}

const categoryOrder: Record<StatusCategory, number> = { todo: 0, in_progress: 1, done: 2 };

/**
 * insertionIndex suggests where a new column for a status of `category` goes: after the last
 * column holding statuses of the same or an earlier category, so a to-do status lands among
 * the to-do columns and a done one at the end.
 */
export function insertionIndex(cols: ColumnDraft[], category: StatusCategory, categoryOf: (statusId: string) => StatusCategory | undefined): number {
  let at = 0;
  cols.forEach((c, i) => {
    const cats = c.statusIds.map(categoryOf).filter((x): x is StatusCategory => !!x);
    if (cats.length === 0) return;
    if (Math.min(...cats.map((x) => categoryOrder[x])) <= categoryOrder[category]) at = i + 1;
  });
  return at;
}

// ---------------------------------------------------------------------------

/**
 * applyBoard is the optimistic board data after a configuration change: issues regroup into
 * the new columns by status (merged columns re-sorted by rank), WIP flags are recomputed and
 * statuses that left the board count as unmapped. Issues of newly mapped statuses aren't
 * loaded yet; the refetch that follows a save brings them.
 */
export function applyBoard(data: BoardData, board: Board): BoardData {
  const issues = data.columns.flatMap((c) => c.issues);
  const from = new Map<string, string>();
  for (const c of data.columns) for (const i of c.issues) from.set(i.id, c.id);
  const columns = board.columns.map((c) => {
    const statuses = new Set(c.statusIds);
    let mine = issues.filter((i) => statuses.has(i.status.id));
    if (new Set(mine.map((i) => from.get(i.id))).size > 1) mine = [...mine].sort((a, b) => (a.rank < b.rank ? -1 : a.rank > b.rank ? 1 : 0));
    return { id: c.id ?? "", name: c.name, statusIds: c.statusIds, ...(c.wipLimit ? { wipLimit: c.wipLimit } : {}), issues: mine, overLimit: c.wipLimit != null && mine.length > c.wipLimit };
  });
  const mapped = new Set(board.columns.flatMap((c) => c.statusIds));
  const unmapped = new Set(data.unmappedStatusIds.filter((s) => !mapped.has(s)));
  for (const c of data.columns) for (const s of c.statusIds) if (!mapped.has(s)) unmapped.add(s);
  return { ...data, board, columns, unmappedStatusIds: [...unmapped] };
}

/**
 * columnErrors sorts a problem's field errors for the columns editor: "columns[2].name" or
 * "columns.2" belong to the third column, plain "columns" to the editor as a whole.
 */
export function columnErrors(fields: Record<string, string>): { general?: string; byIndex: Record<number, string> } {
  const out: { general?: string; byIndex: Record<number, string> } = { byIndex: {} };
  for (const [field, message] of Object.entries(fields)) {
    if (field === "columns") {
      out.general = message;
      continue;
    }
    const m = /^columns(?:\[(\d+)\]|\.(\d+))/.exec(field);
    if (m) out.byIndex[Number(m[1] ?? m[2])] ??= message;
  }
  return out;
}

/** joinNames lists names as "A", "A and B" or "A, B and C". */
export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}
