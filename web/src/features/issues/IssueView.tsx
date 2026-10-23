/**
 * IssueView is the full issue view: header, description, sub-tasks, links, comments,
 * attachments, history, development panel and the details sidebar. Used by the /browse page
 * and inside the board/backlog/search drawer (`variant="drawer"`, with `onClose`).
 * `onNavigate` lets a drawer switch to another issue (e.g. clicking a sub-task) in place.
 */
export function IssueView(_props: { issueKey: string; variant?: "page" | "drawer"; onClose?: () => void; onNavigate?: (key: string) => void }) {
  return null;
}
