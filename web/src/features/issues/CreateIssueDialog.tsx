/**
 * CreateIssueDialog creates an issue. Opened from the top bar ("Create"), boards and backlogs.
 * `projectKey` preselects the project; `defaults` pre-fills fields (e.g. sprintId, parentKey).
 */
export function CreateIssueDialog(_props: {
  open: boolean;
  onClose: () => void;
  projectKey?: string;
  defaults?: { typeKey?: string; sprintId?: string; parentKey?: string; statusId?: string };
  onCreated?: (issue: { key: string }) => void;
}) {
  return null;
}
