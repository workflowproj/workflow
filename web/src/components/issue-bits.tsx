import { ArrowDown, ArrowUp, Bookmark, Bug, ChevronsDown, ChevronsUp, CircleCheck, Equal, Layers, SquareCheck, Zap } from "lucide-react";
import type { Priority, StatusCategory } from "@/api/types";
import { cn } from "@/lib/cn";

const typeIcons: Record<string, typeof Bug> = {
  epic: Zap,
  story: Bookmark,
  task: SquareCheck,
  bug: Bug,
  subtask: Layers,
};

/** IssueTypeIcon draws an issue type's icon in its colour (unknown icons fall back to a check). */
export function IssueTypeIcon({ type, className }: { type: { icon: string; color: string; name: string }; className?: string }) {
  const Icon = typeIcons[type.icon] ?? CircleCheck;
  return (
    <span title={type.name} className={cn("inline-flex size-4 shrink-0 items-center justify-center rounded-[3px] text-white", className)} style={{ background: type.color }}>
      <Icon className="size-3" strokeWidth={2.5} aria-label={type.name} />
    </span>
  );
}

const priorityMeta: Record<Priority, { icon: typeof ArrowUp; color: string; label: string }> = {
  highest: { icon: ChevronsUp, color: "text-danger", label: "Highest" },
  high: { icon: ArrowUp, color: "text-danger", label: "High" },
  medium: { icon: Equal, color: "text-warning", label: "Medium" },
  low: { icon: ArrowDown, color: "text-progress", label: "Low" },
  lowest: { icon: ChevronsDown, color: "text-progress", label: "Lowest" },
};

/** PriorityIcon shows a priority; pass `decorative` when the priority's name is shown next to it. */
export function PriorityIcon({ priority, className, decorative }: { priority: Priority; className?: string; decorative?: boolean }) {
  const m = priorityMeta[priority] ?? priorityMeta.medium;
  const Icon = m.icon;
  const a11y = decorative ? { "aria-hidden": true } : { role: "img", "aria-label": `${m.label} priority` };
  return <Icon {...a11y} className={cn("size-4 shrink-0", m.color, className)} strokeWidth={2.5} />;
}

export function priorityLabel(p: Priority) {
  return priorityMeta[p]?.label ?? p;
}

const categoryStyle: Record<StatusCategory, string> = {
  todo: "bg-todo-soft text-todo",
  in_progress: "bg-progress-soft text-progress",
  done: "bg-done-soft text-done",
};

export function StatusBadge({ status, className }: { status: { name: string; category: StatusCategory }; className?: string }) {
  return (
    <span className={cn("inline-flex h-5 max-w-full items-center truncate rounded px-1.5 text-[11px] font-bold tracking-wide whitespace-nowrap uppercase", categoryStyle[status.category], className)}>
      {status.name}
    </span>
  );
}

export const categoryLabel: Record<StatusCategory, string> = { todo: "To do", in_progress: "In progress", done: "Done" };
