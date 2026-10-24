import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Timer } from "lucide-react";
import type { IssueDetail } from "@/api/types";
import { Button, PageSpinner, useConfirm } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { formatDateTime, relativeTime } from "@/lib/format";
import { useDeleteWorklog, useFormatDuration, worklogsQuery, type Worklog } from "./api";
import { LogWorkDialog } from "./LogWorkDialog";

/** WorklogList is the issue's work log: who logged how much and when, newest first, with edit and delete. */
export function WorklogList({ issue }: { issue: IssueDetail }) {
  const { data, isLoading, error } = useQuery(worklogsQuery(issue.key));
  const fmt = useFormatDuration();
  const [editing, setEditing] = useState<Worklog | null>(null);
  const [logging, setLogging] = useState(false);
  if (isLoading) return <PageSpinner label="Loading work log" />;
  if (error) return <ErrorBox error={error} />;
  const list = [...(data ?? [])].reverse();
  const total = list.reduce((n, w) => n + w.timeSpentSeconds, 0);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">{list.length === 0 ? "No work logged yet." : `${fmt(total)} logged in ${list.length} entr${list.length === 1 ? "y" : "ies"}.`}</p>
        {issue.permissions.edit && (
          <Button size="sm" icon={<Timer className="size-3.5" />} onClick={() => setLogging(true)}>
            Log work
          </Button>
        )}
      </div>
      {list.length > 0 && (
        <ol className="flex flex-col gap-4" aria-label="Work log">
          {list.map((w) => (
            <WorklogItem key={w.id} worklog={w} issueKey={issue.key} onEdit={() => setEditing(w)} />
          ))}
        </ol>
      )}
      <LogWorkDialog issue={issue} open={logging} onClose={() => setLogging(false)} />
      <LogWorkDialog issue={issue} worklog={editing ?? undefined} open={!!editing} onClose={() => setEditing(null)} />
    </div>
  );
}

function WorklogItem({ worklog: w, issueKey, onEdit }: { worklog: Worklog; issueKey: string; onEdit: () => void }) {
  const fmt = useFormatDuration();
  const confirm = useConfirm();
  const remove = useDeleteWorklog(issueKey);
  const author = w.author ?? { displayName: "Former user", kind: "human" as const };
  return (
    <li className="flex gap-3">
      <UserAvatar user={author} size={28} className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
          <span className="font-medium">{author.displayName}</span>
          <span className="text-muted">logged</span>
          <span className="rounded bg-progress-soft px-1.5 py-0.5 text-[13px] font-semibold text-progress tabular-nums">{fmt(w.timeSpentSeconds)}</span>
          <time dateTime={w.startedAt} title={`Work started ${formatDateTime(w.startedAt)}`} className="text-xs text-muted">
            {formatDateTime(w.startedAt)}
          </time>
          {w.updatedAt !== w.createdAt && (
            <span className="text-xs text-faint" title={`Edited ${formatDateTime(w.updatedAt)}`}>
              (edited)
            </span>
          )}
        </div>
        {w.comment && <p className="mt-1 text-sm break-words whitespace-pre-wrap">{w.comment}</p>}
        <div className="mt-1 flex gap-3 text-xs text-muted">
          <span title={formatDateTime(w.createdAt)}>Logged {relativeTime(w.createdAt)}</span>
          {w.canEdit && (
            <>
              <button type="button" className="hover:text-ink hover:underline" onClick={onEdit}>
                Edit
              </button>
              <button
                type="button"
                className="hover:text-danger hover:underline"
                disabled={remove.isPending}
                onClick={async () => {
                  if (await confirm({ title: "Delete work log?", body: `The ${fmt(w.timeSpentSeconds)} logged goes back on the remaining estimate.`, confirmLabel: "Delete", danger: true })) remove.mutate(w.id);
                }}
              >
                Delete
              </button>
            </>
          )}
        </div>
      </div>
    </li>
  );
}
