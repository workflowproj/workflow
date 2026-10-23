import type { KeyboardEvent } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { CircleCheck, CircleDot, CircleX, CornerDownRight, GitBranch, GitMerge, GitPullRequest } from "lucide-react";
import type { Issue } from "@/api/types";
import { IssueTypeIcon, PriorityIcon } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { formatPoints } from "./model";

/** IssueCard is the visual card on a board; SortableIssueCard makes it draggable and openable. */
export function IssueCard({ issue, lifted, className }: { issue: Issue; lifted?: boolean; className?: string }) {
  const done = issue.status.category === "done";
  return (
    <div
      className={cn(
        "rounded-md border border-line bg-surface p-2.5 text-left shadow-[0_1px_1px_rgb(0_0_0/0.04)] transition-colors",
        lifted ? "cursor-grabbing border-line-strong shadow-xl ring-1 ring-accent/30" : "hover:border-line-strong hover:bg-surface-2/40",
        className,
      )}
    >
      <p className={cn("line-clamp-3 text-[13px] leading-snug text-ink", done && "text-muted")}>{issue.summary}</p>
      <CardChips issue={issue} />
      <div className="mt-2.5 flex items-center gap-1.5">
        <IssueTypeIcon type={issue.type} />
        <span className={cn("font-mono text-[11px] font-medium text-muted", done && "line-through")}>{issue.key}</span>
        <PriorityIcon priority={issue.priority} className="size-3.5" />
        <DevIndicators dev={issue.dev} />
        <span className="ml-auto" />
        {issue.storyPoints != null && <PointsPill points={issue.storyPoints} />}
        <UserAvatar user={issue.assignee} size={22} />
      </div>
    </div>
  );
}

export function PointsPill({ points, className }: { points: number; className?: string }) {
  return (
    <span title={`${formatPoints(points)} story points`} className={cn("inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-surface-3 px-1.5 text-[11px] font-semibold text-muted tabular-nums", className)}>
      {formatPoints(points)}
    </span>
  );
}

/** ParentChip shows an issue's epic (with its colour) or, for a sub-task, the parent key. */
export function ParentChip({ parent, className }: { parent: NonNullable<Issue["parent"]>; className?: string }) {
  const epic = parent.type.level >= 1;
  return (
    <span title={`${parent.type.name} ${parent.key}: ${parent.summary}`} className={cn("inline-flex h-5 max-w-full min-w-0 items-center gap-1 rounded bg-surface-2 px-1.5 text-[11px] font-medium text-muted", className)}>
      {epic ? <span aria-hidden className="size-2 shrink-0 rounded-[2px]" style={{ background: parent.type.color }} /> : <CornerDownRight className="size-3 shrink-0" aria-hidden />}
      <span className="truncate">{epic ? parent.summary : parent.key}</span>
    </span>
  );
}

function CardChips({ issue }: { issue: Issue }) {
  if (!issue.parent && issue.labels.length === 0) return null;
  const labels = issue.labels.slice(0, 3);
  return (
    <div className="mt-2 flex flex-wrap items-center gap-1">
      {issue.parent && <ParentChip parent={issue.parent} className="max-w-[12rem]" />}
      {labels.map((l) => (
        <span key={l} className="inline-flex h-5 max-w-[8rem] items-center truncate rounded border border-line px-1.5 text-[11px] text-muted">
          {l}
        </span>
      ))}
      {issue.labels.length > labels.length && <span className="text-[11px] text-faint">+{issue.labels.length - labels.length}</span>}
    </div>
  );
}

const pipelineMeta: Record<string, { icon: typeof CircleCheck; className: string; label: string }> = {
  success: { icon: CircleCheck, className: "text-success", label: "Pipeline passed" },
  failed: { icon: CircleX, className: "text-danger", label: "Pipeline failed" },
  running: { icon: CircleDot, className: "text-warning", label: "Pipeline running" },
  pending: { icon: CircleDot, className: "text-faint", label: "Pipeline pending" },
  canceled: { icon: CircleX, className: "text-faint", label: "Pipeline canceled" },
};

/** DevIndicators summarises development activity: pull requests (or a branch) and the latest pipeline. */
export function DevIndicators({ dev }: { dev?: Issue["dev"] }) {
  if (!dev) return null;
  const pipeline = dev.pipelineState ? pipelineMeta[dev.pipelineState] : undefined;
  let pr: { icon: typeof GitBranch; className: string; label: string; count?: number } | null = null;
  if (dev.openPullRequests > 0) pr = { icon: GitPullRequest, className: "text-progress", label: `${dev.openPullRequests} open pull request${dev.openPullRequests > 1 ? "s" : ""}`, count: dev.openPullRequests };
  else if (dev.mergedPullRequests > 0) pr = { icon: GitMerge, className: "text-done", label: `${dev.mergedPullRequests} merged pull request${dev.mergedPullRequests > 1 ? "s" : ""}`, count: dev.mergedPullRequests };
  else if (dev.branches > 0) pr = { icon: GitBranch, className: "text-muted", label: `${dev.branches} branch${dev.branches > 1 ? "es" : ""}` };
  if (!pr && !pipeline) return null;
  return (
    <span className="inline-flex items-center gap-1">
      {pr && (
        <span title={pr.label} className={cn("inline-flex items-center gap-0.5 text-[11px] font-medium", pr.className)}>
          <pr.icon className="size-3.5" aria-label={pr.label} />
          {pr.count != null && pr.count > 1 && pr.count}
        </span>
      )}
      {pipeline && (
        <span title={pipeline.label} className="inline-flex">
          <pipeline.icon className={cn("size-3.5", pipeline.className)} aria-label={pipeline.label} />
        </span>
      )}
    </span>
  );
}

/**
 * SortableIssueCard: drag with the pointer, or focus it and press Space to pick it up (arrows
 * move, Space drops, Escape cancels). Click or Enter opens the issue.
 */
export function SortableIssueCard({ issue, onOpen, disabled, dimmed }: { issue: Issue; onOpen: (key: string) => void; disabled?: boolean; dimmed?: boolean }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: issue.id, disabled });
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Enter" && !isDragging) {
      e.preventDefault();
      onOpen(issue.key);
      return;
    }
    listeners?.onKeyDown?.(e);
  };
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      {...attributes}
      {...listeners}
      onKeyDown={onKeyDown}
      onClick={() => onOpen(issue.key)}
      aria-label={`${issue.key} ${issue.summary}`}
      data-issue-key={issue.key}
      className={cn(
        "cursor-pointer rounded-md outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-canvas",
        !disabled && "touch-none",
        isDragging && "opacity-35",
        dimmed && "opacity-60",
      )}
    >
      <IssueCard issue={issue} />
    </div>
  );
}
