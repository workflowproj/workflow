import { useQuery } from "@tanstack/react-query";
import { IssueTypeIcon, PriorityIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { ErrorBox } from "@/components/ErrorBox";
import { Spinner } from "@/components/ui";
import { cn } from "@/lib/cn";
import { releaseIssuesQuery, type Release } from "./queries";

/** ReleaseIssues lists the issues whose fix version is the release (JQL search). */
export function ReleaseIssues({ projectKey, release, onOpen }: { projectKey: string; release: Release; onOpen: (key: string) => void }) {
  const { data, isLoading, error } = useQuery(releaseIssuesQuery(projectKey, release.name));
  if (isLoading)
    return (
      <div className="flex items-center gap-2 px-4 py-3 text-sm text-muted">
        <Spinner /> Loading issues…
      </div>
    );
  if (error) return <ErrorBox error={error} className="m-3" />;
  const issues = data?.issues ?? [];
  if (issues.length === 0) return <p className="px-4 py-3 text-sm text-muted">No issues have this release as their fix version yet.</p>;
  return (
    <ul className="divide-y divide-line">
      {issues.map((i) => {
        const done = i.status.category === "done";
        return (
          <li key={i.id}>
            <button type="button" onClick={() => onOpen(i.key)} className="flex w-full items-center gap-2.5 px-4 py-2 text-left text-sm hover:bg-surface-2 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent">
              <IssueTypeIcon type={i.type} />
              <span className={cn("w-16 shrink-0 font-mono text-xs text-muted", done && "line-through")}>{i.key}</span>
              <span className={cn("min-w-0 flex-1 truncate", done && "text-muted")}>{i.summary}</span>
              <PriorityIcon priority={i.priority} className="size-3.5" />
              <StatusBadge status={i.status} className="max-w-[9rem]" />
              <UserAvatar user={i.assignee} size={22} />
            </button>
          </li>
        );
      })}
      {data && data.total > issues.length && <li className="px-4 py-2 text-xs text-muted">Showing {issues.length} of {data.total} issues.</li>}
    </ul>
  );
}
