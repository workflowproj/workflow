import type { CSSProperties, KeyboardEvent, PointerEvent } from "react";
import type { Issue } from "@/api/types";
import { cn } from "@/lib/cn";
import type { Schedule } from "./model";

export const ROW_H = 36;
export const BAR_H = 20;
export const BAR_TOP = (ROW_H - BAR_H) / 2;

export type DragMode = "move" | "start" | "end";

const categoryVar: Record<string, string> = { todo: "var(--color-todo)", in_progress: "var(--color-progress)", done: "var(--color-done)" };

/**
 * barLook styles a bar by status category. Bars with their own dates are filled (to-do ones
 * lightly); derived dates (sprint, children, a lone date) are hatched with a dashed edge.
 */
function barLook(issue: Issue, s: Schedule, epic: boolean): { className: string; style: CSSProperties } {
  const color = categoryVar[issue.status.category] ?? categoryVar.todo;
  if (s.source !== "own")
    return {
      className: "border-[1.5px] border-dashed text-ink",
      style: {
        borderColor: color,
        backgroundColor: `color-mix(in srgb, ${color} 10%, var(--color-surface))`,
        backgroundImage: `repeating-linear-gradient(135deg, color-mix(in srgb, ${color} 30%, transparent) 0 2px, transparent 2px 7px)`,
      },
    };
  if (epic || issue.status.category === "todo")
    return { className: "border text-ink", style: { borderColor: `color-mix(in srgb, ${color} 60%, transparent)`, backgroundColor: `color-mix(in srgb, ${color} 22%, var(--color-surface))` } };
  return { className: "border border-transparent text-accent-ink", style: { backgroundColor: color } };
}

export function Bar({
  issue,
  schedule: s,
  left,
  width,
  progress,
  editable,
  selected,
  conflict,
  dragging,
  linkTarget,
  onPointerDown,
  onKeyDown,
  onFocus,
  onHover,
}: {
  issue: Issue;
  schedule: Schedule;
  left: number;
  width: number;
  progress?: { done: number; total: number };
  editable: boolean;
  selected: boolean;
  conflict: boolean;
  dragging: boolean;
  linkTarget: boolean;
  onPointerDown: (e: PointerEvent, mode: DragMode) => void;
  onKeyDown: (e: KeyboardEvent) => void;
  onFocus: () => void;
  onHover: (on: boolean) => void;
}) {
  const epic = issue.type.level >= 1;
  const look = barLook(issue, s, epic);
  const pct = progress && progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : null;
  const inside = width >= 76;
  const handle = (mode: DragMode) =>
    editable &&
    width >= 14 && (
      <span
        aria-hidden
        onPointerDown={(e) => {
          e.stopPropagation();
          onPointerDown(e, mode);
        }}
        className={cn("absolute inset-y-0 z-[1] flex w-2 cursor-ew-resize items-center justify-center", mode === "start" ? "left-0" : "right-0")}
      >
        <span className="h-2.5 w-0.5 rounded-full bg-current opacity-0 transition-opacity group-hover/bar:opacity-50" />
      </span>
    );
  return (
    <>
      <button
        type="button"
        data-bar-key={issue.key}
        aria-label={`${issue.key} ${issue.summary}`}
        onPointerDown={(e) => onPointerDown(e, "move")}
        onKeyDown={onKeyDown}
        onFocus={onFocus}
        onPointerEnter={() => onHover(true)}
        onPointerLeave={() => onHover(false)}
        className={cn(
          "group/bar absolute z-[2] flex items-center overflow-hidden rounded-[5px] text-left text-[11px] leading-none font-medium whitespace-nowrap outline-none select-none",
          look.className,
          editable ? "cursor-grab active:cursor-grabbing" : "cursor-pointer",
          dragging && "shadow-lg ring-2 ring-accent/50",
          selected && !dragging && "ring-2 ring-accent ring-offset-1 ring-offset-surface",
          conflict && !selected && !dragging && "ring-2 ring-danger/70",
          linkTarget && "ring-2 ring-accent",
          "focus-visible:ring-2 focus-visible:ring-accent",
        )}
        style={{ ...look.style, left, width: Math.max(width, 4), top: BAR_TOP, height: BAR_H, touchAction: "none" }}
      >
        {pct != null && (
          <span aria-hidden className="absolute inset-y-0 left-0 bg-done/35" style={{ width: `${pct}%` }} />
        )}
        {handle("start")}
        {inside && <span className="relative truncate px-2">{issue.summary}</span>}
        {inside && pct != null && <span className="relative ml-auto shrink-0 pr-2 tabular-nums opacity-80">{pct}%</span>}
        {handle("end")}
      </button>
      {!inside && (
        <span aria-hidden className="pointer-events-none absolute z-[2] max-w-56 truncate text-[11px] leading-none text-muted" style={{ left: left + Math.max(width, 4) + (editable ? 18 : 6), top: BAR_TOP + 5 }}>
          {issue.summary}
          {pct != null && ` · ${pct}%`}
        </span>
      )}
    </>
  );
}

/** LinkHandle starts a dependency drag from the end of a bar. */
export function LinkHandle({ left, visible, issueKey, onPointerDown }: { left: number; visible: boolean; issueKey: string; onPointerDown: (e: PointerEvent) => void }) {
  return (
    <button
      type="button"
      aria-label={`Drag to another issue to make ${issueKey} block it`}
      title="Drag onto another bar to add a dependency (this blocks that)"
      onPointerDown={onPointerDown}
      className={cn(
        "absolute z-[6] flex size-3 cursor-crosshair items-center justify-center rounded-full border-2 border-accent bg-surface transition-opacity focus-visible:opacity-100",
        visible ? "opacity-100" : "opacity-0 group-hover/row:opacity-100",
      )}
      style={{ left: left + 4, top: ROW_H / 2 - 6, touchAction: "none" }}
    />
  );
}
