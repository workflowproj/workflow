import { ChevronRight, Clock, TriangleAlert } from "lucide-react";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { ProjectAvatar } from "@/layout/AppShell";
import { cn } from "@/lib/cn";
import type { Row } from "./model";

export const TREE_W = 380;

/**
 * TreeCell is the left part of a timeline row: a project, a group ("No epic") or an issue
 * with its key, type, summary, status and assignee. Clicking an issue opens it.
 */
export function TreeCell({
  row,
  collapsed,
  onToggle,
  onOpen,
  warnings,
  overdue,
}: {
  row: Row;
  collapsed: boolean;
  onToggle: () => void;
  onOpen: (key: string) => void;
  warnings?: string[];
  overdue?: boolean;
}) {
  const chevron = (label: string) => (
    <button type="button" onClick={onToggle} aria-expanded={!collapsed} aria-label={`${collapsed ? "Expand" : "Collapse"} ${label}`} className="flex size-5 shrink-0 items-center justify-center rounded text-faint hover:bg-surface-3 hover:text-ink focus-visible:outline-2 focus-visible:outline-accent">
      <ChevronRight className={cn("size-3.5 transition-transform", !collapsed && "rotate-90")} />
    </button>
  );
  if (row.kind === "project")
    return (
      <div className="flex h-full items-center gap-2 px-2">
        {chevron(row.project.name)}
        <ProjectAvatar projectKey={row.project.key} size={20} />
        <span className="truncate text-sm font-semibold">{row.project.name}</span>
        <span className="font-mono text-xs text-faint">{row.project.key}</span>
        <span className="ml-auto text-xs text-faint tabular-nums">{row.count}</span>
      </div>
    );
  if (row.kind === "group")
    return (
      <div className="flex h-full items-center gap-2 px-2" style={{ paddingLeft: 8 + row.depth * 16 }}>
        {chevron(row.label)}
        <span className={cn("truncate text-[13px] font-medium", row.parent ? "text-ink" : "text-muted")}>{row.label}</span>
        {row.parent && <span className="font-mono text-xs text-faint">{row.parent.key}</span>}
        <span className="ml-auto text-xs text-faint tabular-nums">{row.count}</span>
      </div>
    );
  const i = row.issue;
  const done = i.status.category === "done";
  const epic = i.type.level >= 1;
  return (
    <div className="flex h-full items-center gap-1.5 pr-2" style={{ paddingLeft: 8 + row.depth * 16 }}>
      {row.childCount > 0 ? chevron(i.key) : <span className="size-5 shrink-0" />}
      <button
        type="button"
        onClick={() => onOpen(i.key)}
        className="flex min-w-0 flex-1 items-center gap-2 rounded py-1 text-left focus-visible:outline-2 focus-visible:outline-accent"
        title={`${i.key} ${i.summary}`}
      >
        <IssueTypeIcon type={i.type} />
        <span className={cn("shrink-0 font-mono text-[11px] text-muted", done && "line-through")}>{i.key}</span>
        <span className={cn("min-w-0 flex-1 truncate text-[13px]", epic && "font-medium", done && "text-muted")}>{i.summary}</span>
      </button>
      {warnings && warnings.length > 0 && (
        <span title={warnings.join("\n")} className="inline-flex shrink-0 text-danger">
          <TriangleAlert className="size-3.5" aria-label={`Dependency conflict: ${warnings.join(" ")}`} />
        </span>
      )}
      {overdue && (
        <span title="Overdue: the due date has passed" className="inline-flex shrink-0 text-warning">
          <Clock className="size-3.5" aria-label="Overdue" />
        </span>
      )}
      <StatusBadge status={i.status} className="max-w-[5.5rem] shrink-0" />
      <UserAvatar user={i.assignee} size={20} />
    </div>
  );
}
