import { useEffect, useRef, useState } from "react";
import { ArrowRight, Trash2, X } from "lucide-react";
import type { Issue } from "@/api/types";
import { Button, IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { Schedule, TimelineLink } from "./model";
import { fromDay } from "./model";

const dateInput =
  "h-7 w-[8.5rem] rounded-md border border-line-strong bg-surface px-2 text-[13px] text-ink focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none";

/**
 * DatePopover is the precise (and keyboard) way to schedule: it appears under a selected bar
 * with its start and due dates. Derived dates are offered as a starting point.
 */
export function DatePopover({
  issue,
  schedule,
  left,
  editable,
  onSave,
  onOpen,
}: {
  issue: Issue;
  schedule?: Schedule;
  left: number;
  editable: boolean;
  onSave: (start: string | null, due: string | null) => void;
  onOpen: () => void;
}) {
  const [start, setStart] = useState(issue.startDate ?? (schedule ? fromDay(schedule.start) : ""));
  const [due, setDue] = useState(issue.dueDate ?? (schedule ? fromDay(schedule.end) : ""));
  useEffect(() => {
    setStart(issue.startDate ?? (schedule ? fromDay(schedule.start) : ""));
    setDue(issue.dueDate ?? (schedule ? fromDay(schedule.end) : ""));
    // Re-seed only when the issue's saved dates change, not while typing.
  }, [issue.startDate, issue.dueDate]);
  const invalid = !!start && !!due && due < start;
  const changed = start !== (issue.startDate ?? "") || due !== (issue.dueDate ?? "");
  return (
    <form
      data-date-popover
      onSubmit={(e) => {
        e.preventDefault();
        if (!invalid && changed) onSave(start || null, due || null);
      }}
      onPointerDown={(e) => e.stopPropagation()}
      className="absolute z-30 flex flex-wrap items-end gap-2 rounded-lg border border-line bg-surface p-2.5 shadow-xl"
      style={{ left: Math.max(4, left), top: 36 - 2 }}
    >
      <label className="flex flex-col gap-1 text-[11px] font-medium text-muted">
        Start
        <input type="date" className={dateInput} value={start} max={due || undefined} disabled={!editable} onChange={(e) => setStart(e.target.value)} aria-label={`Start date of ${issue.key}`} />
      </label>
      <label className="flex flex-col gap-1 text-[11px] font-medium text-muted">
        Due
        <input type="date" className={cn(dateInput, invalid && "border-danger")} value={due} min={start || undefined} disabled={!editable} onChange={(e) => setDue(e.target.value)} aria-label={`Due date of ${issue.key}`} />
      </label>
      {editable && (
        <>
          <Button type="submit" size="sm" variant="primary" disabled={invalid || !changed}>
            Save
          </Button>
          {(issue.startDate || issue.dueDate) && (
            <Button size="sm" variant="ghost" onClick={() => onSave(null, null)}>
              Clear
            </Button>
          )}
        </>
      )}
      <Button size="sm" variant="ghost" onClick={onOpen}>
        Open
      </Button>
      {schedule && schedule.source !== "own" && <p className="w-full max-w-72 text-[11px] text-muted">{schedule.note}</p>}
      {invalid && <p className="w-full text-[11px] text-danger">The due date is before the start date.</p>}
      {!editable && <p className="w-full text-[11px] text-muted">You can't edit issues in this project.</p>}
    </form>
  );
}

/** LinkMenu explains a clicked dependency arrow and offers to remove it. */
export function LinkMenu({
  link,
  conflict,
  left,
  top,
  summaries,
  canRemove,
  onRemove,
  onOpen,
  onClose,
}: {
  link: TimelineLink;
  conflict: boolean;
  left: number;
  top: number;
  summaries: Map<string, string>;
  canRemove: boolean;
  onRemove: () => void;
  onOpen: (key: string) => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
    const onDown = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && onClose();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("pointerdown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);
  const issue = (key: string) => (
    <button type="button" onClick={() => onOpen(key)} className="min-w-0 rounded px-1 text-left hover:bg-surface-2">
      <span className="font-mono text-xs font-semibold">{key}</span>
      <span className="block truncate text-xs text-muted">{summaries.get(key) ?? ""}</span>
    </button>
  );
  return (
    <div ref={ref} tabIndex={-1} role="dialog" aria-label={`${link.sourceKey} blocks ${link.targetKey}`} className="absolute z-40 w-72 rounded-lg border border-line bg-surface p-3 shadow-xl outline-none" style={{ left, top }}>
      <div className="mb-2 flex items-center justify-between">
        <p className="text-xs font-semibold tracking-wide text-faint uppercase">Dependency</p>
        <IconButton label="Close" size="sm" className="size-6" onClick={onClose}>
          <X className="size-3.5" />
        </IconButton>
      </div>
      <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-1.5">
        {issue(link.sourceKey)}
        <span className="flex flex-col items-center text-[10px] text-muted">
          blocks
          <ArrowRight className="size-3.5" />
        </span>
        {issue(link.targetKey)}
      </div>
      {conflict && <p className="mt-2 rounded bg-danger-soft px-2 py-1.5 text-xs text-danger">Conflict: {link.sourceKey} ends after {link.targetKey} starts.</p>}
      {canRemove && (
        <Button size="sm" variant="ghost" className="mt-2 w-full justify-start text-danger" icon={<Trash2 className="size-3.5" />} onClick={onRemove}>
          Remove dependency
        </Button>
      )}
    </div>
  );
}
