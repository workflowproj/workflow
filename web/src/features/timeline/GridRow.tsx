import { memo, type KeyboardEvent, type PointerEvent } from "react";
import type { Schemas } from "@/api/client";
import type { Issue } from "@/api/types";
import { cn } from "@/lib/cn";
import { Bar, BAR_H, BAR_TOP, LinkHandle, ROW_H, type DragMode } from "./Bar";
import { preview, type BarDrag, type CreateDrag } from "./drag";
import { MarkerLane } from "./Markers";
import { formatSpan, type Row, type Schedule } from "./model";
import { DatePopover } from "./Popovers";
import { TREE_W, TreeCell } from "./TreeCell";

/** Stable callbacks from the grid (they read its live state), so rows can be memoised. */
export type RowActions = {
  toggle: (id: string) => void;
  open: (key: string) => void;
  startBar: (e: PointerEvent, issue: Issue, mode: DragMode) => void;
  startLink: (e: PointerEvent, issue: Issue, rowIndex: number) => void;
  startCreate: (e: PointerEvent, issue: Issue) => void;
  hint: (key: string, clientX: number | null) => void;
  barKey: (e: KeyboardEvent, issue: Issue) => void;
  select: (key: string, on: boolean) => void;
  hover: (key: string, on: boolean) => void;
  save: (issue: Issue, start: string | null, due: string | null) => void;
};

export type Markers = { sprints: Schemas["Sprint"][]; releases: Schemas["Release"][] };

/**
 * GridRow is one timeline row: the sticky tree cell and the row's lane in the grid. It is
 * memoised: scrolling only renders the rows that come into view, and a drag only the dragged row.
 */
export const GridRow = memo(function GridRow({
  row,
  index,
  gridW,
  x,
  ppd,
  today,
  collapsed,
  warnings,
  overdue,
  schedule,
  editable,
  progress,
  selected,
  hint,
  barDrag,
  createDrag,
  linkTarget,
  dragging,
  markers,
  actions,
}: {
  row: Row;
  index: number;
  gridW: number;
  x: (day: number) => number;
  ppd: number;
  today: number;
  collapsed: boolean;
  warnings?: string[];
  overdue: boolean;
  schedule?: Schedule;
  editable: boolean;
  progress?: { done: number; total: number };
  selected: boolean;
  hint?: number;
  barDrag: BarDrag | null;
  createDrag: CreateDrag | null;
  linkTarget: boolean;
  dragging: boolean;
  markers?: Markers;
  actions: RowActions;
}) {
  return (
    <div data-row-key={row.id} className="group/row absolute left-0 flex" style={{ top: index * ROW_H, height: ROW_H, width: TREE_W + gridW }}>
      <div
        className={cn(
          "sticky left-0 z-10 shrink-0 border-r border-b border-line bg-surface group-hover/row:bg-surface-2",
          row.kind === "project" && "bg-surface-2",
          selected && "bg-accent-soft/60",
        )}
        style={{ width: TREE_W }}
      >
        <TreeCell row={row} collapsed={collapsed} onToggle={() => actions.toggle(row.id)} onOpen={actions.open} warnings={warnings} overdue={overdue} />
      </div>
      <div className={cn("relative border-b border-line/50 group-hover/row:bg-surface-2/40", row.kind === "project" && "bg-surface-2/70")} style={{ width: gridW }}>
        {row.kind === "project" && markers && <MarkerLane sprints={markers.sprints} releases={markers.releases} x={x} ppd={ppd} today={today} height={ROW_H} />}
        {row.kind === "issue" && (
          <IssueLane
            issue={row.issue}
            index={index}
            schedule={schedule}
            x={x}
            ppd={ppd}
            editable={editable}
            progress={progress}
            conflict={!!warnings?.length}
            selected={selected}
            hint={hint}
            barDrag={barDrag}
            createDrag={createDrag}
            linkTarget={linkTarget}
            dragging={dragging}
            actions={actions}
          />
        )}
      </div>
    </div>
  );
});

function DateChip({ x, start, end }: { x: (day: number) => number; start: number; end: number }) {
  return (
    <span className="pointer-events-none absolute z-30 rounded bg-ink px-1.5 py-0.5 text-[10px] leading-none font-semibold whitespace-nowrap text-surface shadow" style={{ left: x(start), top: -6 }}>
      {formatSpan(start, end)} · {end - start + 1}d
    </span>
  );
}

/** IssueLane is the grid part of an issue row: its bar (with drag preview), handles, hint and popover. */
function IssueLane({
  issue,
  index,
  schedule,
  x,
  ppd,
  editable,
  progress,
  conflict,
  selected,
  hint,
  barDrag,
  createDrag,
  linkTarget,
  dragging,
  actions,
}: {
  issue: Issue;
  index: number;
  schedule?: Schedule;
  x: (day: number) => number;
  ppd: number;
  editable: boolean;
  progress?: { done: number; total: number };
  conflict: boolean;
  selected: boolean;
  hint?: number;
  barDrag: BarDrag | null;
  createDrag: CreateDrag | null;
  linkTarget: boolean;
  dragging: boolean;
  actions: RowActions;
}) {
  const moving = barDrag && barDrag.editable && barDrag.moved ? barDrag : null;
  const shown = schedule && moving ? { ...schedule, ...preview(moving) } : schedule;

  if (!shown) {
    // Unscheduled: drag across the row to set its dates.
    const a = createDrag ? Math.min(createDrag.anchor, createDrag.current) : undefined;
    const b = createDrag ? Math.max(createDrag.anchor, createDrag.current) : undefined;
    return (
      <div
        className={cn("absolute inset-0", editable && "cursor-cell")}
        onPointerDown={editable ? (e) => actions.startCreate(e, issue) : undefined}
        onPointerMove={editable && !dragging ? (e) => actions.hint(issue.key, e.clientX) : undefined}
        onPointerLeave={() => actions.hint(issue.key, null)}
      >
        {a != null && b != null ? (
          <>
            <span className="absolute rounded-[5px] border-[1.5px] border-dashed border-accent bg-accent-soft/60" style={{ left: x(a), width: (b - a + 1) * ppd, top: BAR_TOP, height: BAR_H }} />
            <DateChip x={x} start={a} end={b} />
          </>
        ) : (
          hint != null && (
            <>
              <span className="pointer-events-none absolute rounded-[5px] border border-dashed border-line-strong" style={{ left: x(hint), width: Math.max(ppd, 6), top: BAR_TOP, height: BAR_H }} />
              <span className="pointer-events-none absolute text-[11px] whitespace-nowrap text-muted" style={{ left: x(hint) + Math.max(ppd, 6) + 6, top: BAR_TOP + 4 }}>
                Drag to schedule
              </span>
            </>
          )
        )}
      </div>
    );
  }
  const left = x(shown.start);
  const width = (shown.end - shown.start + 1) * ppd;
  return (
    <span
      className="contents"
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) actions.select(issue.key, false);
      }}
    >
      <Bar
        issue={issue}
        schedule={shown}
        left={left}
        width={width}
        progress={issue.type.level >= 1 ? progress : undefined}
        editable={editable}
        selected={selected}
        conflict={conflict}
        dragging={!!moving}
        linkTarget={linkTarget}
        onPointerDown={(e, mode) => actions.startBar(e, issue, mode)}
        onKeyDown={(e) => actions.barKey(e, issue)}
        onFocus={() => actions.select(issue.key, true)}
        onHover={(on) => actions.hover(issue.key, on)}
      />
      {moving && <DateChip x={x} start={shown.start} end={shown.end} />}
      {editable && !dragging && <LinkHandle left={left + Math.max(width, 4)} visible={selected} issueKey={issue.key} onPointerDown={(e) => actions.startLink(e, issue, index)} />}
      {selected && !dragging && (
        <DatePopover issue={issue} schedule={schedule} left={left} editable={editable} onSave={(s, d) => actions.save(issue, s, d)} onOpen={() => actions.open(issue.key)} />
      )}
    </span>
  );
}
