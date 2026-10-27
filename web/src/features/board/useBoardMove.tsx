import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/api/client";
import type { Issue } from "@/api/types";
import { useToast } from "@/components/ui";
import { TransitionScreenDialog } from "@/features/issues/TransitionScreenDialog";
import { needsScreen, neighbours, placeOnBoard, planDrop, type RankHint } from "./model";
import { moveError, moveIssue, type MoveRequest } from "./moveIssue";
import { boardDataQuery, transitionsQuery, type AvailableTransition, type BoardColumnData, type BoardData } from "./queries";
import { TransitionChooser } from "./TransitionChooser";

type Pending = {
  issue: Issue;
  column: BoardColumnData;
  hint: RankHint;
  /** place shows the card in the target column (with its new status, when known). */
  place: (status?: Issue["status"]) => void;
  /** rollback puts a placed card back where it was; it does nothing when the card wasn't placed. */
  rollback: () => void;
};

/**
 * useBoardMove handles a card dropped on a board: it picks the transition into the target
 * column (asking when several statuses there are reachable), calls POST /issues/{key}/move
 * and rolls back with a toast if the workflow refuses. A plain move shows the card in its new
 * place straight away; a move that needs a choice or a transition screen leaves the card where
 * it was until the user confirms (TransitionScreenDialog runs the transition, then the rank is
 * re-applied), so cancelling changes nothing.
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

  const openScreen = (p: Pending, transition: AvailableTransition) => {
    screenRef.current = { ...p, transition };
    setScreen(screenRef.current);
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
          // The card waits in its old place while the screen is open.
          p.rollback();
          openScreen(p, t);
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
    const plan = planDrop(issue.status.id, column.statusIds, qc.getQueryData<AvailableTransition[]>(transitionsQuery(issue.key).queryKey));
    if (plan.kind === "refuse") {
      toast.error(new Error(`${issue.key} can't move from ${issue.status.name} to ${column.name}: the workflow has no transition there for you.`));
      return;
    }
    const hint = neighbours(list, issueId);
    const back = neighbours(from.issues.map((i) => i.id), issueId);
    let placed = false;
    const place = (status?: Issue["status"]) => {
      placed = true;
      qc.cancelQueries({ queryKey: ["board", boardId] });
      qc.setQueryData<BoardData>(key, (d) => (d ? placeOnBoard(d, issueId, column.id, hint, status) : d));
    };
    const rollback = () => {
      if (!placed) return;
      placed = false;
      qc.setQueryData<BoardData>(key, (d) => (d ? placeOnBoard(d, issueId, from.id, back, issue.status) : d));
    };
    const pending: Pending = { issue, column, hint, place, rollback };
    switch (plan.kind) {
      case "choose":
        setChoice({ ...pending, options: plan.options });
        return;
      case "screen":
        openScreen(pending, plan.transition);
        return;
      case "rank":
        place();
        void run(pending, undefined);
        return;
      case "move":
        place(plan.transition?.to);
        void run(pending, plan.transition);
    }
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
          if (!c) return;
          if (needsScreen(t)) {
            openScreen(c, t);
            return;
          }
          c.place(t.to);
          void run(c, t);
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
