import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import type { Issue } from "@/api/types";
import { useToast } from "@/components/ui";
import { TransitionScreenDialog } from "@/features/issues/TransitionScreenDialog";
import { neighbours, placeOnBoard, type RankHint } from "./model";
import { moveError, moveIssue, type MoveRequest } from "./moveIssue";
import { boardDataQuery, transitionsQuery, type AvailableTransition, type BoardColumnData, type BoardData } from "./queries";
import { TransitionChooser } from "./TransitionChooser";

type Pending = { issue: Issue; column: BoardColumnData; hint: RankHint; rollback: () => void };

/**
 * useBoardMove handles a card dropped on a board: it picks the transition into the target
 * column (asking when several statuses there are reachable), moves the card optimistically,
 * calls POST /issues/{key}/move and rolls back with a toast if the workflow refuses. A move
 * that needs a transition screen opens TransitionScreenDialog, then re-applies the rank.
 */
export function useBoardMove(boardId: string, jql: string) {
  const qc = useQueryClient();
  const toast = useToast();
  const key = boardDataQuery(boardId, jql).queryKey;
  const [choice, setChoice] = useState<(Pending & { options: AvailableTransition[] }) | null>(null);
  const [screen, setScreen] = useState<(Pending & { transition: AvailableTransition }) | null>(null);
  const screenRef = useRef<typeof screen>(null);

  const refresh = (issueKey: string) => {
    qc.invalidateQueries({ queryKey: ["board", boardId] });
    qc.invalidateQueries({ queryKey: ["backlog"] });
    qc.invalidateQueries({ queryKey: ["issue", issueKey] });
  };

  const run = async (p: Pending, transition: AvailableTransition | null | undefined) => {
    // undefined: same status (rank only); null: transitions unknown, let the API pick by status.
    const body: MoveRequest = { ...p.hint };
    if (transition) Object.assign(body, { transitionId: transition.id, statusId: transition.to.id });
    else if (transition === null) body.statusId = p.column.statusIds[0];
    try {
      const moved = await moveIssue(p.issue.key, body);
      qc.setQueryData(key, (d) => (d ? placeOnBoard(d, moved.id, p.column.id, p.hint, moved.status) : d));
      refresh(p.issue.key);
    } catch (err) {
      if (err instanceof ApiError && err.code === "screen_required") {
        const id = err.problem.errors?.[0]?.message;
        const list = await qc.fetchQuery(transitionsQuery(p.issue.key)).catch(() => []);
        const t = list.find((x) => x.id === id) ?? transition;
        if (t) {
          screenRef.current = { ...p, transition: t };
          setScreen(screenRef.current);
          return;
        }
      }
      p.rollback();
      toast.error(moveError(err));
      refresh(p.issue.key);
    }
  };

  /** drop is called by the board with the target column and the card's new neighbours. */
  const drop = (issueId: string, toColumnId: string, list: string[]) => {
    const data = qc.getQueryData<BoardData>(key);
    const from = data?.columns.find((c) => c.issues.some((i) => i.id === issueId));
    const column = data?.columns.find((c) => c.id === toColumnId);
    const issue = from?.issues.find((i) => i.id === issueId);
    if (!data || !from || !column || !issue) return;
    const hint = neighbours(list, issueId);
    const statusChange = !column.statusIds.includes(issue.status.id);
    const known = qc.getQueryData<AvailableTransition[]>(transitionsQuery(issue.key).queryKey);
    const options = statusChange && known ? known.filter((t) => column.statusIds.includes(t.to.id)) : [];
    if (statusChange && known && options.length === 0) {
      toast.error(new Error(`${issue.key} can't move from ${issue.status.name} to ${column.name}: the workflow has no transition there for you.`));
      return;
    }
    const back = neighbours(from.issues.map((i) => i.id), issueId);
    const rollback = () => qc.setQueryData<BoardData>(key, (d) => (d ? placeOnBoard(d, issueId, from.id, back, issue.status) : d));
    qc.cancelQueries({ queryKey: ["board", boardId] });
    qc.setQueryData<BoardData>(key, placeOnBoard(data, issueId, column.id, hint, options.length === 1 ? options[0].to : undefined));
    const pending: Pending = { issue, column, hint, rollback };
    if (options.length > 1) setChoice({ ...pending, options });
    else void run(pending, statusChange ? (options[0] ?? null) : undefined);
  };

  const closeScreen = (done: boolean, moved?: Issue) => {
    const s = screenRef.current;
    if (!s) return;
    screenRef.current = null;
    setScreen(null);
    if (!done) {
      s.rollback();
      return;
    }
    if (moved) qc.setQueryData<BoardData>(key, (d) => (d ? placeOnBoard(d, moved.id, s.column.id, s.hint, moved.status) : d));
    // The dialog ran the transition; the drop position still needs its rank.
    const rank = s.hint.afterIssueId || s.hint.beforeIssueId ? moveIssue(s.issue.key, s.hint).catch((e) => toast.error(moveError(e))) : Promise.resolve();
    void rank.finally(() => refresh(s.issue.key));
  };

  const dialogs = (
    <>
      <TransitionChooser
        open={!!choice}
        issue={choice?.issue ?? null}
        columnName={choice?.column.name ?? ""}
        options={choice?.options ?? []}
        onPick={(t) => {
          const c = choice;
          setChoice(null);
          if (c) {
            qc.setQueryData<BoardData>(key, (d) => (d ? placeOnBoard(d, c.issue.id, c.column.id, c.hint, t.to) : d));
            void run(c, t);
          }
        }}
        onCancel={() => {
          choice?.rollback();
          setChoice(null);
        }}
      />
      <TransitionScreenDialog
        open={!!screen}
        issueKey={screen?.issue.key ?? ""}
        transition={screen?.transition ?? null}
        onClose={() => closeScreen(false)}
        onDone={(issue) => closeScreen(true, issue)}
      />
    </>
  );

  return { drop, dialogs };
}
