import { api, ApiError, errorMessage, unwrap, type Schemas } from "@/api/client";

export type MoveRequest = Schemas["MoveRequest"];

/** moveIssue is POST /issues/{key}/move: status, sprint and rank in one atomic call. */
export function moveIssue(issueKey: string, body: MoveRequest) {
  return unwrap(api.POST("/issues/{issueKey}/move", { params: { path: { issueKey } }, body }));
}

/**
 * moveError explains a refused move in one line, including validator messages, which the
 * API sends as field-less errors (e.g. "PLAT-9 cannot move to Done yet: Finish every sub-task first.").
 */
export function moveError(err: unknown): Error {
  if (err instanceof ApiError) {
    const reasons = (err.problem.errors ?? []).filter((e) => !e.field).map((e) => e.message);
    const head = err.problem.detail || err.message;
    if (err.status === 403 && !err.problem.detail) return new Error("You don't have permission to move this issue.");
    return new Error(reasons.length ? `${head}: ${reasons.join(" ")}` : head);
  }
  return new Error(errorMessage(err));
}
