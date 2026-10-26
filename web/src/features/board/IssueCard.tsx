import type { KeyboardEvent } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { CalendarDays, CircleCheck, CircleDot, CircleX, Clock3, CornerDownRight, GitBranch, GitMerge, GitPullRequest, Rocket } from "lucide-react";
import type { Issue } from "@/api/types";
import { IssueTypeIcon, PriorityIcon } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { ageDots, cardLayout, daysLabel, daysSince, daysTitle, DEFAULT_CARD_FIELDS, dueLabel, type CardField } from "./card-fields";
import { formatPoints } from "./model";
import { useFormatDuration } from "@/features/time/api";
import { cardTimeLabel } from "@/features/time/model";

/**
 * IssueCard is the visual card on a board; SortableIssueCard makes it draggable and openable.
 * The summary, type and key always show; `fields` (the board's cardFields) add the rest, in order.
 */
export function IssueCard({ issue, fields = DEFAULT_CARD_FIELDS, lifted, className }: { issue: Issue; fields?: CardField[]; lifted?: boolean; className?: string }) {
  const done = issue.status.category === "done";
  const { tags, left, right } = cardLayout(fields);
  return (
    <div
      className={cn(
        "rounded-md border border-line bg-surface p-2.5 text-left shadow-[0_1px_1px_rgb(0_0_0/0.04)] transition-colors",
        lifted ? "cursor-grabbing border-line-strong shadow-xl ring-1 ring-accent/30" : "hover:border-line-strong hover:bg-surface-2/40",
        className,
      )}
    >
      <p className={cn("line-clamp-3 text-[13px] leading-snug break-words text-ink", done && "text-muted")}>{issue.summary}</p>
      <CardChips issue={issue} fields={tags} />
      <div className="mt-2.5 flex items-center gap-1.5">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-1.5 gap-y-1">
          <IssueTypeIcon type={issue.type} />
          <span className={cn("font-mono text-[11px] font-medium text-muted", done && "line-through")}>{issue.key}</span>
          {left.map((f) => (
            <FieldValue key={f} field={f} issue={issue} />
          ))}
        </div>
        {right.map((f) => (
          <FieldValue key={f} field={f} issue={issue} />
        ))}
      </div>
    </div>
  );
}

/** FieldValue renders one optional card field, or nothing when the issue has no value for it. */
function FieldValue({ field, issue }: { field: CardField; issue: Issue }) {
  switch (field) {
    case "priority":
      return <PriorityIcon priority={issue.priority} className="size-3.5" />;
    case "dev":
      return <DevIndicators dev={issue.dev} />;
    case "points":
      return issue.storyPoints != null ? <PointsPill points={issue.storyPoints} /> : null;
    case "assignee":
      return <UserAvatar user={issue.assignee} size={22} />;
    case "dueDate":
      return issue.dueDate ? <DueDate due={issue.dueDate} done={issue.status.category === "done"} /> : null;
    case "daysInColumn":
      return <DaysInColumn issue={issue} />;
    case "timeTracking":
      return <TimeTracking issue={issue} />;
    default:
      return null;
  }
}

function DueDate({ due, done }: { due: string; done: boolean }) {
  const d = dueLabel(due, new Date(), done);
  return (
    <span title={d.title} aria-label={d.title} className={cn("inline-flex h-5 items-center gap-1 rounded px-1 text-[11px] font-medium whitespace-nowrap", d.overdue ? "bg-danger-soft text-danger" : "text-muted")}>
      <CalendarDays className="size-3" aria-hidden />
      {d.text}
    </span>
  );
}

/** TimeTracking: time logged of the original estimate ("3h / 1d"), else what is left; red once over. */
function TimeTracking({ issue }: { issue: Issue }) {
  const fmt = useFormatDuration();
  const t = cardTimeLabel(issue.timeTracking, fmt);
  if (!t) return null;
  return (
    <span title={t.title} aria-label={t.title} className={cn("inline-flex h-5 items-center gap-1 rounded px-1 text-[11px] font-medium whitespace-nowrap tabular-nums", t.over ? "bg-danger-soft text-danger" : "text-muted")}>
      <Clock3 className="size-3" aria-hidden />
      {t.text}
    </span>
  );
}

/** DaysInColumn: days since the issue entered its status, with dots that fill over two weeks. Hidden once done. */
function DaysInColumn({ issue }: { issue: Issue }) {
  const days = daysSince(issue.statusChangedAt, Date.now());
  if (days === undefined || issue.status.category === "done") return null;
  const dots = ageDots(days);
  const title = daysTitle(issue.status.name, days);
  return (
    <span title={title} aria-label={title} className={cn("inline-flex h-5 items-center gap-1 text-[11px] font-medium tabular-nums", dots >= 4 ? "text-warning" : dots > 0 ? "text-muted" : "text-faint")}>
      <span aria-hidden className="inline-flex gap-px">
        {[1, 2, 3, 4].map((n) => (
          <span key={n} className={cn("size-1 rounded-full", n <= dots ? "bg-current" : "bg-line-strong")} />
        ))}
      </span>
      {daysLabel(days)}
    </span>
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

function CardChips({ issue, fields }: { issue: Issue; fields: CardField[] }) {
  const parent = fields.includes("epic") ? issue.parent : undefined;
  const all = fields.includes("labels") ? issue.labels : [];
  if (!parent && all.length === 0) return null;
  const labels = all.slice(0, 3);
  const labelChips = (
    <>
      {labels.map((l) => (
        <span key={l} className="inline-flex h-5 max-w-[8rem] items-center truncate rounded border border-line px-1.5 text-[11px] text-muted">
          {l}
        </span>
      ))}
      {all.length > labels.length && <span className="text-[11px] text-faint">+{all.length - labels.length}</span>}
    </>
  );
  const epicChip = parent && <ParentChip parent={parent} className="max-w-[12rem]" />;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-1">
      {fields.indexOf("labels") < fields.indexOf("epic") ? (
        <>
          {labelChips}
          {epicChip}
        </>
      ) : (
        <>
          {epicChip}
          {labelChips}
        </>
      )}
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
  const deployed = dev.deployedTo?.length ? `Deployed to ${dev.deployedTo.join(", ")}` : undefined;
  if (!pr && !pipeline && !deployed) return null;
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
      {deployed && (
        <span title={deployed} className="inline-flex">
          <Rocket className="size-3.5 text-success" aria-label={deployed} />
        </span>
      )}
    </span>
  );
}

/**
 * SortableIssueCard: drag with the pointer, or focus it and press Space to pick it up (arrows
 * move, Space drops, Escape cancels). Click or Enter opens the issue.
 */
export function SortableIssueCard({
  issue,
  fields,
  onOpen,
  disabled,
  dimmed,
}: {
  issue: Issue;
  fields?: CardField[];
  onOpen: (key: string) => void;
  disabled?: boolean;
  dimmed?: boolean;
}) {
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
      <IssueCard issue={issue} fields={fields} />
    </div>
  );
}
