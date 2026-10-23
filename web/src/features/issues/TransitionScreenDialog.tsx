import type { Schemas } from "@/api/client";

/**
 * TransitionScreenDialog collects a transition's screen fields (and an optional comment) and
 * runs it: POST /issues/{key}/transitions { transitionId, fields, comment }. Used by the issue
 * view's status menu and by boards when a drop answers 422 `screen_required` (the transition id
 * is in problem.errors[0].message; load the issue's available transitions to get its screen).
 */
export function TransitionScreenDialog(_props: {
  open: boolean;
  onClose: () => void;
  issueKey: string;
  transition: Schemas["AvailableTransition"] | null;
  onDone?: (issue: Schemas["Issue"]) => void;
}) {
  return null;
}
