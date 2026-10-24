import type { ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { ChevronRight } from "lucide-react";
import type { Issue } from "@/api/types";
import { ShowMore, useShowMore } from "@/components/ShowMore";
import { cn } from "@/lib/cn";
import { formatPoints, sumPoints } from "@/features/board/model";
import { SortableIssueRow } from "./IssueRow";

/** Rows drawn at first, and added per "Show more". */
const ROWS = 100;

/**
 * BacklogSection is a sprint or the backlog: a collapsible header with counts and story
 * points, then a sortable list of issue rows. The whole section is a drop target, so issues
 * can be dropped on its header, even when it is collapsed.
 */
export function BacklogSection({
  id,
  title,
  badge,
  meta,
  description,
  actions,
  issues,
  items,
  collapsed,
  onToggle,
  onOpen,
  canDrag,
  highlight,
  rowActions,
  empty,
  footer,
  total,
}: {
  id: string;
  title: ReactNode;
  badge?: ReactNode;
  meta?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  /** Issues by id for rendering; `items` is the (possibly mid-drag) order. */
  issues: Map<string, Issue>;
  items: string[];
  collapsed: boolean;
  onToggle: () => void;
  onOpen: (key: string) => void;
  canDrag: boolean;
  highlight?: boolean;
  rowActions?: (issue: Issue) => ReactNode;
  empty: ReactNode;
  footer?: ReactNode;
  /** How many issues the section has when the server sent only the first of them. */
  total?: number;
}) {
  const { setNodeRef } = useDroppable({ id });
  const list = items.map((i) => issues.get(i)).filter((i): i is Issue => !!i);
  const { limit, more } = useShowMore(ROWS);
  const shown = items.length > limit ? items.slice(0, limit) : items;
  return (
    <section ref={setNodeRef} aria-label={typeof title === "string" ? title : undefined} className={cn("rounded-lg border bg-surface transition-shadow", highlight ? "border-accent/50 ring-2 ring-accent/20" : "border-line")}>
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-2.5">
        <button type="button" onClick={onToggle} aria-expanded={!collapsed} className="flex min-w-0 items-center gap-1.5 rounded text-left focus-visible:outline-2 focus-visible:outline-accent">
          <ChevronRight className={cn("size-4 shrink-0 text-faint transition-transform", !collapsed && "rotate-90")} />
          <h2 className="truncate text-sm font-semibold">{title}</h2>
        </button>
        {badge}
        {meta && <span className="text-xs text-muted">{meta}</span>}
        <span className="text-xs text-faint">
          {total != null && total > list.length ? `${list.length.toLocaleString()} of ${total.toLocaleString()} issues` : `${list.length} issue${list.length === 1 ? "" : "s"}`}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <PointsBreakdown issues={list} />
          {actions}
        </div>
        {description && !collapsed && <p className="w-full pl-5.5 text-xs text-muted">{description}</p>}
      </header>
      {!collapsed && (
        <>
          <SortableContext id={id} items={shown} strategy={verticalListSortingStrategy}>
            <div className={cn("border-t border-line", items.length === 0 && "p-2")}>
              {items.length === 0 ? (
                <div className="flex min-h-14 items-center justify-center rounded-md border border-dashed border-line-strong px-4 text-center text-[13px] text-muted">{empty}</div>
              ) : (
                shown.map((iid) => {
                  const issue = issues.get(iid);
                  return issue ? <SortableIssueRow key={iid} issue={issue} onOpen={onOpen} disabled={!canDrag} actions={rowActions?.(issue)} /> : null;
                })
              )}
            </div>
          </SortableContext>
          <ShowMore total={items.length} limit={limit} page={ROWS} onMore={more} className="w-full border-t border-line px-3 py-2 text-[13px] font-medium text-accent hover:bg-surface-2/60 focus-visible:outline-2 focus-visible:outline-accent" />
          {footer && <div className="border-t border-line">{footer}</div>}
        </>
      )}
    </section>
  );
}

/** PointsBreakdown shows story points by status category (to do, in progress, done). */
export function PointsBreakdown({ issues }: { issues: Issue[] }) {
  const p = sumPoints(issues);
  const pills = [
    { label: "To do", value: p.todo, className: "bg-todo-soft text-todo" },
    { label: "In progress", value: p.in_progress, className: "bg-progress-soft text-progress" },
    { label: "Done", value: p.done, className: "bg-done-soft text-done" },
  ];
  return (
    <div className="flex items-center gap-1" title={`${formatPoints(p.total)} story points`}>
      {pills.map((x) => (
        <span key={x.label} title={`${x.label}: ${formatPoints(x.value)} points`} className={cn("inline-flex h-5 min-w-6 items-center justify-center rounded-full px-1.5 text-[11px] font-semibold tabular-nums", x.className)}>
          <span className="sr-only">{x.label}: </span>
          {formatPoints(x.value)}
        </span>
      ))}
    </div>
  );
}
