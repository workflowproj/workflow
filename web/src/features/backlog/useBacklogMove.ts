import { useQueryClient } from "@tanstack/react-query";
import type { Issue } from "@/api/types";
import { useToast } from "@/components/ui";
import { BACKLOG, neighbours, placeInBacklog } from "@/features/board/model";
import { moveError, moveIssue, type MoveRequest } from "@/features/board/moveIssue";
import { backlogQuery, type Backlog } from "@/features/board/queries";

function containersOf(data: Backlog): { id: string; issues: Issue[] }[] {
  return [...data.sprints.map((s) => ({ id: s.sprint.id, issues: s.issues })), { id: BACKLOG, issues: data.backlog }];
}

/**
 * useBacklogMove moves issues between sprints and the backlog and re-ranks them:
 * optimistic cache update, POST /issues/{key}/move (sprintId null = backlog), rollback on error.
 */
export function useBacklogMove(boardId: string, jql: string) {
  const qc = useQueryClient();
  const toast = useToast();
  const key = backlogQuery(boardId, jql).queryKey;

  /** drop places `issueId` in container `to`, whose final order is `list`. */
  const drop = (issueId: string, to: string, list: string[]) => {
    const data = qc.getQueryData<Backlog>(key);
    const from = data && containersOf(data).find((c) => c.issues.some((i) => i.id === issueId));
    const issue = from?.issues.find((i) => i.id === issueId);
    if (!data || !from || !issue) return;
    const hint = neighbours(list, issueId);
    const body: MoveRequest = { ...hint };
    if (to !== from.id) body.sprintId = to === BACKLOG ? null : to;
    if (Object.keys(body).length === 0) return;
    const back = neighbours(
      from.issues.map((i) => i.id),
      issueId,
    );
    qc.cancelQueries({ queryKey: ["backlog", boardId] });
    qc.setQueryData<Backlog>(key, placeInBacklog(data, issueId, to, hint));
    const refresh = () => {
      qc.invalidateQueries({ queryKey: ["backlog", boardId] });
      qc.invalidateQueries({ queryKey: ["board"] });
    };
    moveIssue(issue.key, body)
      .then(refresh)
      .catch((err) => {
        qc.setQueryData<Backlog>(key, (d) => (d ? placeInBacklog(d, issueId, from.id, back) : d));
        toast.error(moveError(err));
        refresh();
      });
  };

  /** moveTo is the menu alternative to dragging: to the top or bottom of a sprint or the backlog. */
  const moveTo = (issueId: string, to: string, where: "top" | "bottom") => {
    const data = qc.getQueryData<Backlog>(key);
    const target = data && containersOf(data).find((c) => c.id === to);
    if (!target) return;
    const ids = target.issues.map((i) => i.id).filter((id) => id !== issueId);
    drop(issueId, to, where === "top" ? [issueId, ...ids] : [...ids, issueId]);
  };

  return { drop, moveTo };
}
