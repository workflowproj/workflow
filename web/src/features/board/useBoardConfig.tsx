import { useRef } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import type { Status } from "@/api/types";
import { useConfirm, useToast } from "@/components/ui";
import { applyBoard, assignStatus, boardInput, columnInputs, draftColumns, insertColumn, insertionIndex, joinNames, moveColumn, removeColumn, renameColumn, setWipLimit, withChanges, type ColumnDraft } from "./board-config";
import type { ColumnTools } from "./ColumnMenu";
import type { Board, BoardData } from "./queries";

/**
 * useSaveBoard saves a board's whole configuration with PUT /boards/{id}. The change shows at
 * once (the boards list and every loaded view of the board are updated optimistically) and is
 * rolled back if the server refuses it. Saves run one at a time, each sending the board as the
 * earlier ones left it; once the last one settles, the board, its data and backlog refetch.
 */
export function useSaveBoard(projectKey: string, boardId: string) {
  const qc = useQueryClient();
  const mutationKey = ["board-config", boardId];
  return useMutation({
    mutationKey,
    scope: { id: `board-config:${boardId}` },
    mutationFn: (next: Board) => unwrap(api.PUT("/boards/{boardId}", { params: { path: { boardId } }, body: boardInput(next) })),
    onMutate: async (next) => {
      await qc.cancelQueries({ queryKey: ["board", boardId] });
      const list = qc.getQueryData<Board[]>(["boards", projectKey]);
      const data = qc.getQueriesData<BoardData>({ queryKey: ["board", boardId] });
      qc.setQueryData<Board[]>(["boards", projectKey], (l) => l?.map((b) => (b.id === boardId ? next : b)));
      qc.setQueriesData<BoardData>({ queryKey: ["board", boardId] }, (d) => (d ? applyBoard(d, next) : d));
      return { list, data };
    },
    onError: (_err, _next, ctx) => {
      if (!ctx) return;
      qc.setQueryData(["boards", projectKey], ctx.list);
      for (const [key, d] of ctx.data) qc.setQueryData(key, d);
    },
    onSuccess: (saved) => {
      // A later save is still queued: keep showing its optimistic state.
      if (qc.isMutating({ mutationKey }) <= 1) qc.setQueryData<Board[]>(["boards", projectKey], (l) => l?.map((b) => (b.id === saved.id ? saved : b)));
    },
    onSettled: () => {
      if (qc.isMutating({ mutationKey }) > 1) return;
      for (const key of [["boards", projectKey], ["board", boardId], ["backlog", boardId]]) qc.invalidateQueries({ queryKey: key });
    },
  });
}

/** readable turns field errors ("columns[1].name: …") into a sentence for a toast. */
function readable(err: unknown) {
  const fields = err instanceof ApiError && !err.problem.detail ? (err.problem.errors ?? []) : [];
  return fields.length ? new Error(`Couldn't change the board: ${fields.map((f) => f.message).join("; ")}`) : err;
}

/**
 * useColumnTools backs the column header menus and the unmapped-status notice: each edit is
 * applied to the board as currently shown and saved straight away. Deleting a column asks
 * first when it holds statuses, and its toast offers an undo.
 */
export function useColumnTools({ board, statuses, onConfigure }: { board: Board; statuses: Map<string, Status>; onConfigure: () => void }) {
  const save = useSaveBoard(board.projectKey, board.id);
  const toast = useToast();
  const confirm = useConfirm();
  // The board as the next edit should see it: the latest render, or an edit sent since.
  const latest = useRef(board);
  const seen = useRef(board);
  if (seen.current !== board) latest.current = seen.current = board;

  const change = (edit: (cols: ColumnDraft[]) => ColumnDraft[], done?: (undo: () => void) => void) => {
    const base = latest.current;
    const before = draftColumns(base.columns);
    const after = edit(before);
    if (JSON.stringify(columnInputs(after)) === JSON.stringify(columnInputs(before))) return;
    const next = withChanges(base, { columns: after });
    latest.current = next;
    save.mutate(next, {
      onError: (err) => toast.error(readable(err)),
      onSuccess: () => done?.(() => change(() => before)),
    });
  };
  const columnOf = (id: string) => latest.current.columns.find((c) => c.id === id);

  const tools: ColumnTools = {
    rename: (id, name) => change((cols) => renameColumn(cols, id, name)),
    setWipLimit: (id, limit) => change((cols) => setWipLimit(cols, id, limit)),
    move: (id, by) =>
      change((cols) => {
        const i = cols.findIndex((c) => c.key === id);
        return moveColumn(cols, id, i + by);
      }),
    addAfter: (id, name) =>
      change((cols) => {
        const i = cols.findIndex((c) => c.key === id);
        return insertColumn(cols, i + 1, name).columns;
      }),
    remove: async (id) => {
      const column = columnOf(id);
      if (!column) return;
      const names = column.statusIds.map((s) => statuses.get(s)?.name ?? "Unknown");
      if (
        names.length > 0 &&
        !(await confirm({
          title: `Delete the ${column.name} column?`,
          body: `Issues in ${joinNames(names)} won't show on the board until ${names.length > 1 ? "those statuses are" : "that status is"} mapped to another column. Issues themselves don't change.`,
          confirmLabel: "Delete column",
          danger: true,
        }))
      )
        return;
      change(
        (cols) => removeColumn(cols, id),
        (undo) => toast.success(`Deleted the ${column.name} column`, { label: "Undo", onClick: undo }),
      );
    },
    configure: onConfigure,
  };

  /** mapStatus puts an unmapped status on the board: into a column, or a new column named after it. */
  const mapStatus = (statusId: string, columnId: string | "new") => {
    const status = statuses.get(statusId);
    change((cols) => {
      if (columnId !== "new") return assignStatus(cols, statusId, columnId);
      const at = insertionIndex(cols, status?.category ?? "todo", (s) => statuses.get(s)?.category);
      return insertColumn(cols, at, status?.name ?? "New column", [statusId]).columns;
    });
  };

  return { tools, mapStatus };
}
