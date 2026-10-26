import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { StatusCategory } from "@/api/types";
import { IssueTypeIcon, PriorityIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { ErrorBox } from "@/components/ErrorBox";
import { Select, Spinner } from "@/components/ui";
import { cn } from "@/lib/cn";
import { releaseIssuesQuery, type Release } from "./queries";

const CATEGORIES: { id: StatusCategory | "all"; label: string }[] = [
  { id: "all", label: "All" },
  { id: "todo", label: "To do" },
  { id: "in_progress", label: "In progress" },
  { id: "done", label: "Done" },
];

/**
 * ReleaseIssues lists the issues whose fix version is the release (JQL search). With `filters`
 * it can be narrowed to a status category and an issue type.
 */
export function ReleaseIssues({ projectKey, release, onOpen, filters = false }: { projectKey: string; release: Pick<Release, "name">; onOpen: (key: string) => void; filters?: boolean }) {
  const { data, isLoading, error } = useQuery(releaseIssuesQuery(projectKey, release.name));
  const [category, setCategory] = useState<StatusCategory | "all">("all");
  const [type, setType] = useState("");
  if (isLoading)
    return (
      <div className="flex items-center gap-2 px-4 py-3 text-sm text-muted">
        <Spinner /> Loading issues…
      </div>
    );
  if (error) return <ErrorBox error={error} className="m-3" />;
  const all = data?.issues ?? [];
  if (all.length === 0) return <p className="px-4 py-3 text-sm text-muted">No issues have this release as their fix version yet.</p>;
  const types = [...new Map(all.map((i) => [i.type.key, i.type])).values()];
  const issues = all.filter((i) => (category === "all" || i.status.category === category) && (!type || i.type.key === type));
  return (
    <>
      {filters && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line px-3 py-2">
          <div role="radiogroup" aria-label="Status" className="flex flex-wrap gap-1">
            {CATEGORIES.map((c) => {
              const n = c.id === "all" ? all.length : all.filter((i) => i.status.category === c.id).length;
              return (
                <button
                  key={c.id}
                  type="button"
                  role="radio"
                  aria-checked={category === c.id}
                  onClick={() => setCategory(c.id)}
                  className={cn(
                    "inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-[13px] font-medium",
                    category === c.id ? "bg-accent-soft text-accent" : "text-muted hover:bg-surface-2 hover:text-ink",
                  )}
                >
                  {c.label}
                  <span className="text-xs opacity-75 tabular-nums">{n}</span>
                </button>
              );
            })}
          </div>
          {types.length > 1 && (
            <Select aria-label="Issue type" value={type} onChange={(e) => setType(e.target.value)} className="h-7 w-40 text-[13px]">
              <option value="">All types</option>
              {types.map((t) => (
                <option key={t.key} value={t.key}>
                  {t.name}
                </option>
              ))}
            </Select>
          )}
        </div>
      )}
      {issues.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">No issues match.</p>
      ) : (
        <ul className="divide-y divide-line">
          {issues.map((i) => {
            const done = i.status.category === "done";
            return (
              <li key={i.id}>
                <button
                  type="button"
                  onClick={() => onOpen(i.key)}
                  className="flex w-full items-center gap-2.5 px-4 py-2 text-left text-sm hover:bg-surface-2 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
                >
                  <IssueTypeIcon type={i.type} />
                  <span className={cn("min-w-16 shrink-0 font-mono text-xs whitespace-nowrap text-muted", done && "line-through")}>{i.key}</span>
                  <span className={cn("min-w-0 flex-1 truncate", done && "text-muted")}>{i.summary}</span>
                  <PriorityIcon priority={i.priority} className="size-3.5" />
                  <StatusBadge status={i.status} className="max-w-[9rem]" />
                  <UserAvatar user={i.assignee} size={22} />
                </button>
              </li>
            );
          })}
          {data && data.total > all.length && <li className="px-4 py-2 text-xs text-muted">Showing {all.length} of {data.total} issues.</li>}
        </ul>
      )}
    </>
  );
}
