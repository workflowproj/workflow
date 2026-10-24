import type { KeyboardEvent, ReactNode } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical } from "lucide-react";
import type { Issue } from "@/api/types";
import { IssueTypeIcon, PriorityIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { DevIndicators, ParentChip, PointsPill } from "@/features/board/IssueCard";

/**
 * IssueRow is one line of a sprint or the backlog. `actions` renders at the end (e.g. a menu);
 * `reserve` keeps that space empty when the actions are rendered outside (SortableIssueRow).
 */
export function IssueRow({ issue, lifted, actions, reserve }: { issue: Issue; lifted?: boolean; actions?: ReactNode; reserve?: boolean }) {
  const done = issue.status.category === "done";
  return (
    <div className={cn("group flex h-10 items-center gap-2.5 bg-surface pr-2 pl-1.5 text-sm", lifted ? "cursor-grabbing rounded-md border border-line-strong shadow-xl ring-1 ring-accent/30" : "hover:bg-surface-2/60")}>
      <GripVertical className="size-3.5 shrink-0 text-faint opacity-0 group-hover:opacity-100" aria-hidden />
      <IssueTypeIcon type={issue.type} />
      <span className={cn("w-16 shrink-0 font-mono text-xs text-muted", done && "line-through")}>{issue.key}</span>
      <span className={cn("min-w-0 flex-1 truncate", done && "text-muted")}>{issue.summary}</span>
      {issue.parent && <ParentChip parent={issue.parent} className="hidden max-w-[10rem] md:inline-flex" />}
      <DevIndicators dev={issue.dev} />
      <StatusBadge status={issue.status} className="hidden max-w-[8.5rem] sm:inline-flex" />
      <span className="flex w-7 justify-center">{issue.storyPoints != null ? <PointsPill points={issue.storyPoints} /> : <span className="text-xs text-faint">–</span>}</span>
      <PriorityIcon priority={issue.priority} className="size-3.5" />
      <UserAvatar user={issue.assignee} size={22} />
      {actions ? <span className="flex w-7 justify-center">{actions}</span> : reserve && <span className="w-7" aria-hidden />}
    </div>
  );
}

/**
 * SortableIssueRow drags with the pointer or Space (arrows move, Space drops); click or Enter
 * opens. The actions sit beside the row's button, not inside it, so no control is nested in another.
 */
export function SortableIssueRow({ issue, onOpen, disabled, actions }: { issue: Issue; onOpen: (key: string) => void; disabled?: boolean; actions?: ReactNode }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: issue.id, disabled });
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return;
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
      className={cn("group relative border-b border-line last:border-b-0", isDragging && "opacity-35")}
    >
      <div
        {...attributes}
        {...listeners}
        onKeyDown={onKeyDown}
        onClick={() => onOpen(issue.key)}
        aria-label={`${issue.key} ${issue.summary}`}
        data-issue-key={issue.key}
        className={cn("cursor-pointer outline-none focus-visible:relative focus-visible:z-[1] focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset", !disabled && "touch-none")}
      >
        <IssueRow issue={issue} reserve={!!actions} />
      </div>
      {actions && <span className="absolute top-1/2 right-2 flex w-7 -translate-y-1/2 justify-center">{actions}</span>}
    </div>
  );
}
