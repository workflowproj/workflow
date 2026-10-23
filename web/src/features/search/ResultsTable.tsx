import { Link } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, SearchX } from "lucide-react";
import type { Issue } from "@/api/types";
import { Button, EmptyState } from "@/components/ui";
import { IssueTypeIcon, PriorityIcon, priorityLabel, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { formatDateTime, relativeTime } from "@/lib/format";
import { currentSort } from "./jql";

type Column = { id: string; label: string; sort: string; desc?: boolean; className?: string; srOnly?: boolean };

const COLUMNS: Column[] = [
  { id: "type", label: "Type", sort: "type", className: "w-11", srOnly: true },
  { id: "key", label: "Key", sort: "key", className: "w-28" },
  { id: "summary", label: "Summary", sort: "summary" },
  { id: "status", label: "Status", sort: "status", className: "w-40" },
  { id: "priority", label: "Priority", sort: "priority", desc: true, className: "w-28" },
  { id: "assignee", label: "Assignee", sort: "assignee", className: "w-44" },
  { id: "updated", label: "Updated", sort: "updated", desc: true, className: "w-28" },
  { id: "points", label: "Points", sort: "storyPoints", desc: true, className: "w-20 text-right" },
];

const norm = (s: string) => s.toLowerCase().replace(/[\s_-]/g, "");
const sortAliases: Record<string, string> = { issuetype: "type", issuekey: "key", points: "storypoints", updateddate: "updated" };

/**
 * ResultsTable lists search results. Clicking a column header re-sorts (the navigator rewrites
 * ORDER BY); clicking a row, or Enter on a focused row, opens the issue.
 */
export function ResultsTable({
  jql,
  issues,
  total,
  selectedKey,
  loading,
  hasMore,
  loadingMore,
  onOpen,
  onSort,
  onLoadMore,
  dimmed,
}: {
  jql: string;
  issues: Issue[];
  total: number;
  selectedKey?: string;
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  onOpen: (key: string) => void;
  onSort: (field: string, defaultDesc: boolean) => void;
  onLoadMore: () => void;
  dimmed?: boolean;
}) {
  const sort = currentSort(jql);
  const sortField = sort ? (sortAliases[norm(sort.field)] ?? norm(sort.field)) : undefined;
  return (
    <div className="overflow-hidden rounded-lg border border-line bg-surface">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[820px] table-fixed border-collapse text-sm">
          <thead>
            <tr className="border-b border-line bg-surface-2/50">
              {COLUMNS.map((c) => {
                const active = sortField === norm(c.sort);
                return (
                  <th key={c.id} scope="col" className={cn("px-3 py-0 text-left text-xs font-semibold text-muted", c.className)} aria-sort={active ? (sort?.desc ? "descending" : "ascending") : undefined}>
                    <button
                      type="button"
                      onClick={() => onSort(c.sort, !!c.desc)}
                      title={`Sort by ${c.label.toLowerCase()}`}
                      className={cn("-mx-1.5 inline-flex h-9 items-center gap-1 rounded px-1.5 hover:text-ink", c.id === "points" && "flex-row-reverse", active && "text-ink")}
                    >
                      <span className={cn(c.srOnly && "sr-only")}>{c.label}</span>
                      {active && (sort?.desc ? <ArrowDown className="size-3.5" /> : <ArrowUp className="size-3.5" />)}
                    </button>
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody className={cn("transition-opacity", dimmed && "opacity-60")}>
            {loading && issues.length === 0
              ? Array.from({ length: 8 }, (_, i) => <SkeletonRow key={i} />)
              : issues.map((issue) => <Row key={issue.id} issue={issue} selected={issue.key === selectedKey} onOpen={onOpen} />)}
          </tbody>
        </table>
      </div>
      {!loading && issues.length === 0 && (
        <EmptyState icon={<SearchX className="size-8" />} title="No issues match this search" body="Try removing a filter, widening a date range or checking the spelling of names and keys." />
      )}
      {issues.length > 0 && (
        <div className="flex items-center justify-between gap-3 border-t border-line px-3 py-2 text-[13px] text-muted">
          <span>
            {issues.length < total ? (
              <>
                Showing <span className="font-medium text-ink tabular-nums">{issues.length}</span> of <span className="font-medium text-ink tabular-nums">{total}</span> issues
              </>
            ) : (
              <>
                <span className="font-medium text-ink tabular-nums">{total}</span> {total === 1 ? "issue" : "issues"}
              </>
            )}
          </span>
          {hasMore && (
            <Button size="sm" onClick={onLoadMore} loading={loadingMore}>
              Load more
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

function Row({ issue, selected, onOpen }: { issue: Issue; selected: boolean; onOpen: (key: string) => void }) {
  const done = issue.status.category === "done";
  return (
    <tr
      tabIndex={0}
      aria-selected={selected}
      onClick={() => onOpen(issue.key)}
      onKeyDown={(e) => {
        const row = e.currentTarget;
        if (e.key === "Enter") onOpen(issue.key);
        else if (e.key === "ArrowDown" || e.key === "j") {
          e.preventDefault();
          (row.nextElementSibling as HTMLElement | null)?.focus();
        } else if (e.key === "ArrowUp" || e.key === "k") {
          e.preventDefault();
          (row.previousElementSibling as HTMLElement | null)?.focus();
        }
      }}
      className={cn(
        "cursor-pointer border-b border-line last:border-b-0 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent",
        selected ? "bg-accent-soft" : "hover:bg-surface-2",
      )}
    >
      <td className="px-3 py-2">
        <IssueTypeIcon type={issue.type} />
      </td>
      <td className="px-3 py-2">
        <Link
          to="/browse/$issueKey"
          params={{ issueKey: issue.key }}
          tabIndex={-1}
          onClick={(e) => {
            e.stopPropagation();
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
            e.preventDefault();
            onOpen(issue.key);
          }}
          className={cn("font-mono text-xs whitespace-nowrap text-muted hover:text-accent hover:underline", done && "line-through")}
        >
          {issue.key}
        </Link>
      </td>
      <td className="px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate font-medium text-ink" title={issue.summary}>
            {issue.summary}
          </span>
          {issue.labels.slice(0, 2).map((l) => (
            <span key={l} className="hidden shrink-0 rounded bg-surface-2 px-1.5 text-[11px] text-muted xl:inline">
              {l}
            </span>
          ))}
        </div>
        {issue.parent && <p className="truncate text-xs text-faint">{issue.parent.key} · {issue.parent.summary}</p>}
      </td>
      <td className="px-3 py-2">
        <StatusBadge status={issue.status} />
      </td>
      <td className="px-3 py-2">
        <span className="flex items-center gap-1.5 text-[13px] text-muted">
          <PriorityIcon priority={issue.priority} />
          {priorityLabel(issue.priority)}
        </span>
      </td>
      <td className="px-3 py-2">
        <span className="flex min-w-0 items-center gap-2 text-[13px]">
          <UserAvatar user={issue.assignee} size={20} />
          <span className={cn("truncate", !issue.assignee && "text-faint")}>{issue.assignee?.displayName ?? "Unassigned"}</span>
        </span>
      </td>
      <td className="px-3 py-2 text-[13px] whitespace-nowrap text-muted" title={formatDateTime(issue.updatedAt)}>
        {relativeTime(issue.updatedAt)}
      </td>
      <td className="px-3 py-2 text-right text-[13px] text-muted tabular-nums">{issue.storyPoints ?? <span className="text-faint">–</span>}</td>
    </tr>
  );
}

function SkeletonRow() {
  return (
    <tr className="border-b border-line last:border-b-0">
      {COLUMNS.map((c) => (
        <td key={c.id} className="px-3 py-3">
          <div className={cn("h-3 animate-pulse rounded bg-surface-3", c.id === "summary" ? "w-3/4" : c.id === "type" ? "size-4" : "w-2/3")} />
        </td>
      ))}
    </tr>
  );
}
