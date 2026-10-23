import { useMemo, useState, type ReactNode } from "react";
import { DndContext, DragOverlay, useDroppable, type Announcements, type UniqueIdentifier } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, CircleCheck } from "lucide-react";
import type { Status } from "@/api/types";
import { IssueTypeIcon, PriorityIcon } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { IssueCard, SortableIssueCard } from "./IssueCard";
import { buildLanes, cellId, findContainer, parseCellId, type Containers, type Lane, type SwimlaneMode } from "./model";
import { transitionsQuery, type BoardColumnData } from "./queries";
import { useContainerDnd } from "./useContainerDnd";

/**
 * BoardView renders a board's columns (optionally split into swimlanes) and lets cards be
 * dragged between columns and re-ordered. Cards stay in their swimlane. While a card is
 * dragged, columns it cannot reach under the workflow are dimmed.
 */
export function BoardView({
  columns,
  mode,
  meId,
  statuses,
  canDrag,
  onOpen,
  onDrop,
}: {
  columns: BoardColumnData[];
  mode: SwimlaneMode;
  meId?: string;
  statuses: Map<string, Status>;
  canDrag: boolean;
  onOpen: (key: string) => void;
  onDrop: (issueId: string, toColumnId: string, list: string[]) => void;
}) {
  const lanes = useMemo(() => buildLanes(columns, mode, meId), [columns, mode, meId]);
  const issues = useMemo(() => new Map(columns.flatMap((c) => c.issues.map((i) => [i.id, i] as const))), [columns]);
  const containers = useMemo(() => {
    const out: Containers = {};
    for (const lane of lanes) for (const c of columns) out[cellId(lane.id, c.id)] = lane.cells[c.id].map((i) => i.id);
    return out;
  }, [lanes, columns]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  const dnd = useContainerDnd({
    containers,
    canDrop: (from, to) => parseCellId(from).laneId === parseCellId(to).laneId,
    onDrop: ({ id, to, list }) => onDrop(id, parseCellId(to).columnId, list),
  });
  const active = dnd.activeId ? issues.get(dnd.activeId) : undefined;
  const target = dnd.activeId ? findContainer(dnd.items, dnd.activeId) : undefined;

  // Loading the dragged issue's transitions tells us which columns it can reach.
  const { data: transitions } = useQuery({ ...transitionsQuery(active?.key ?? ""), enabled: !!active });
  const reachable = useMemo(() => {
    if (!active || !transitions) return null;
    const targets = new Set([active.status.id, ...transitions.map((t) => t.to.id)]);
    return new Set(columns.filter((c) => c.statusIds.some((s) => targets.has(s))).map((c) => c.id));
  }, [active, transitions, columns]);

  const columnName = (id: UniqueIdentifier | undefined) => {
    const container = id != null ? findContainer(dnd.items, String(id)) : undefined;
    if (!container) return "";
    const { laneId, columnId } = parseCellId(container);
    const col = columns.find((c) => c.id === columnId)?.name ?? "";
    const lane = lanes.find((l) => l.id === laneId);
    return mode === "none" || !lane ? col : `${col}, ${lane.label}`;
  };
  const keyOf = (id: UniqueIdentifier) => issues.get(String(id))?.key ?? "issue";
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${keyOf(active.id)} in ${columnName(active.id)}.`,
    onDragOver: ({ active, over }) => (over ? `${keyOf(active.id)} is over ${columnName(over.id)}.` : `${keyOf(active.id)} is not over a column.`),
    onDragEnd: ({ active, over }) => (over ? `${keyOf(active.id)} was dropped in ${columnName(over.id)}.` : `${keyOf(active.id)} was dropped.`),
    onDragCancel: ({ active }) => `Moving ${keyOf(active.id)} was cancelled.`,
  };

  const grid = { gridTemplateColumns: `repeat(${columns.length}, minmax(16.5rem, 1fr))` };
  const lanesOn = mode !== "none";

  const cells = (lane: Lane, fill?: boolean) =>
    columns.map((c) => {
      const id = cellId(lane.id, c.id);
      const ids = dnd.items[id] ?? [];
      return (
        <Cell key={id} id={id} items={ids} fill={fill} danger={fill && c.overLimit} over={target === id && !!active} dimmed={!!reachable && !reachable.has(c.id)} header={fill ? <ColumnHeader column={c} statuses={statuses} /> : undefined}>
          {ids.map((iid) => {
            const issue = issues.get(iid);
            return issue ? <SortableIssueCard key={iid} issue={issue} onOpen={onOpen} disabled={!canDrag} /> : null;
          })}
        </Cell>
      );
    });

  return (
    <DndContext
      {...dnd.contextProps}
      accessibility={{
        announcements,
        screenReaderInstructions: {
          draggable: "To pick up an issue, press Space. Use the arrow keys to move it, Space to drop it and Escape to cancel. Press Enter to open it.",
        },
      }}
    >
      <div className="flex min-h-full min-w-max flex-col px-4 pb-4 md:px-6">
        {!lanesOn ? (
          <div className="grid flex-1 gap-3" style={grid}>
            {cells(lanes[0], true)}
          </div>
        ) : (
          <>
            <div className="sticky top-0 z-10 grid gap-3 bg-canvas pb-1" style={grid}>
              {columns.map((c) => (
                <div key={c.id} className={cn("rounded-lg px-1.5", c.overLimit ? "bg-danger-soft" : "bg-surface-2")}>
                  <ColumnHeader column={c} statuses={statuses} />
                </div>
              ))}
            </div>
            {lanes.map((lane) => {
              const closed = collapsed.has(lane.id);
              return (
                <section key={lane.id} className="mt-3" aria-label={`Swimlane ${lane.label}`}>
                  <button
                    type="button"
                    aria-expanded={!closed}
                    onClick={() => setCollapsed((s) => (s.has(lane.id) ? new Set([...s].filter((x) => x !== lane.id)) : new Set(s).add(lane.id)))}
                    className="sticky left-4 flex max-w-[calc(100vw-4rem)] items-center gap-2 rounded-md px-1.5 py-1 text-sm hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent md:left-6"
                  >
                    <ChevronRight className={cn("size-4 text-faint transition-transform", !closed && "rotate-90")} />
                    <LaneIcon lane={lane} mode={mode} />
                    <span className="truncate font-medium">{lane.label}</span>
                    {lane.epic && <span className="font-mono text-xs text-faint">{lane.epic.key}</span>}
                    <span className="text-xs text-faint">
                      {lane.count} issue{lane.count === 1 ? "" : "s"}
                    </span>
                  </button>
                  {!closed && (
                    <div className="mt-1.5 grid gap-3" style={grid}>
                      {cells(lane)}
                    </div>
                  )}
                </section>
              );
            })}
          </>
        )}
      </div>
      <DragOverlay dropAnimation={{ duration: 160, easing: "cubic-bezier(0.2, 0, 0, 1)" }}>{active ? <IssueCard issue={active} lifted /> : null}</DragOverlay>
    </DndContext>
  );
}

function Cell({ id, items, children, header, fill, danger, over, dimmed }: { id: string; items: string[]; children: ReactNode; header?: ReactNode; fill?: boolean; danger?: boolean; over?: boolean; dimmed?: boolean }) {
  const { setNodeRef } = useDroppable({ id });
  return (
    <div
      className={cn(
        "flex flex-col rounded-lg transition-[opacity,box-shadow]",
        danger ? "bg-danger-soft" : "bg-surface-2",
        fill ? "min-h-64" : "min-h-20",
        over && !dimmed && "ring-2 ring-accent/35",
        dimmed && "opacity-45",
      )}
    >
      {header && <div className={cn("sticky top-0 z-[1] rounded-t-lg px-1.5", danger ? "bg-danger-soft" : "bg-surface-2")}>{header}</div>}
      <SortableContext id={id} items={items} strategy={verticalListSortingStrategy}>
        <div ref={setNodeRef} className={cn("flex flex-1 flex-col gap-1.5 p-1.5", header && "pt-0")}>
          {children}
        </div>
      </SortableContext>
    </div>
  );
}

function ColumnHeader({ column, statuses }: { column: BoardColumnData; statuses: Map<string, Status> }) {
  const count = column.issues.length;
  const done = column.statusIds.length > 0 && column.statusIds.every((s) => statuses.get(s)?.category === "done");
  const names = column.statusIds.map((s) => statuses.get(s)?.name).filter(Boolean);
  return (
    <div className="flex h-10 items-center gap-2 px-1" title={names.length > 1 ? `Statuses: ${names.join(", ")}` : undefined}>
      <h3 className="truncate text-xs font-semibold tracking-wide text-muted uppercase">{column.name}</h3>
      {done && <CircleCheck className="size-3.5 shrink-0 text-done" aria-label="Done column" />}
      {column.wipLimit != null ? (
        <span
          title={column.overLimit ? `Over the work-in-progress limit of ${column.wipLimit}` : `Work-in-progress limit: ${column.wipLimit}`}
          className={cn("ml-auto rounded px-1.5 py-0.5 text-[11px] font-semibold tabular-nums", column.overLimit ? "bg-danger text-white" : "bg-surface-3 text-muted")}
        >
          {count} / {column.wipLimit}
        </span>
      ) : (
        <span className="ml-auto text-xs text-faint tabular-nums">{count}</span>
      )}
    </div>
  );
}

function LaneIcon({ lane, mode }: { lane: Lane; mode: SwimlaneMode }) {
  if (mode === "assignee") return <UserAvatar user={lane.user ?? null} size={20} />;
  if (lane.epic) return <IssueTypeIcon type={lane.epic.type} />;
  if (lane.priority) return <PriorityIcon priority={lane.priority} />;
  if (lane.type) return <IssueTypeIcon type={lane.type} />;
  return null;
}

