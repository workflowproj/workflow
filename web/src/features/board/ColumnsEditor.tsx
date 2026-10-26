import { useState } from "react";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type Announcements,
  type CollisionDetection,
  type DragEndEvent,
  type KeyboardCoordinateGetter,
} from "@dnd-kit/core";
import { rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, GripVertical, Plus, Trash2, TriangleAlert, X } from "lucide-react";
import { statusesQuery } from "@/api/queries";
import type { StatusCategory } from "@/api/types";
import { IconButton, Input, Select } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import { useProjectStatuses } from "@/features/settings/queries";
import { cn } from "@/lib/cn";
import { assignStatus, insertColumn, moveColumn, removeColumn, renameColumn, setWipLimit, type ColumnDraft } from "./board-config";

export type StatusInfo = { id: string; name: string; category: StatusCategory; inProject: boolean };

/**
 * useStatusInfo lists every status a column might reference: the project's work statuses (in
 * workflow order) plus stale ones. Risk and decision statuses never need a column.
 */
export function useStatusInfo(projectKey: string) {
  const project = useProjectStatuses(projectKey, { workOnly: true });
  const { data: all = [] } = useQuery(statusesQuery);
  const statuses = new Map<string, StatusInfo>();
  for (const s of project.statuses) statuses.set(s.id, { id: s.id, name: s.name, category: s.category, inProject: true });
  for (const s of all) if (!statuses.has(s.id)) statuses.set(s.id, { id: s.id, name: s.name, category: s.category, inProject: false });
  return { statuses, loading: project.loading, error: project.error };
}

const COL = "col:";
const ST = "st:";
const TRAY = "unmapped";

// Columns sort among themselves; a status chip drops on a column or the tray.
const collision: CollisionDetection = (args) => {
  const column = String(args.active.id).startsWith(COL);
  const droppableContainers = column ? args.droppableContainers.filter((c) => String(c.id).startsWith(COL)) : args.droppableContainers;
  if (column) return closestCenter({ ...args, droppableContainers });
  const within = pointerWithin({ ...args, droppableContainers });
  return within.length ? within : rectIntersection({ ...args, droppableContainers });
};

// The arrow keys move a column one place; a picked-up status chip jumps to the next drop target.
const keyboardCoordinates: KeyboardCoordinateGetter = (event, args) => {
  const { active, collisionRect, droppableRects, droppableContainers } = args.context;
  if (!active || String(active.id).startsWith(COL)) return sortableKeyboardCoordinates(event, args);
  if (!collisionRect || !["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.code)) return undefined;
  event.preventDefault();
  const cx = collisionRect.left + collisionRect.width / 2;
  const cy = collisionRect.top + collisionRect.height / 2;
  let best: { x: number; y: number; d: number } | undefined;
  for (const c of droppableContainers.getEnabled()) {
    const r = droppableRects.get(c.id);
    if (!r) continue;
    const inside = cx >= r.left && cx <= r.left + r.width && cy >= r.top && cy <= r.top + r.height;
    if (inside) continue;
    const tx = r.left + r.width / 2;
    const ty = r.top + Math.min(r.height / 2, 40);
    const ok = { ArrowLeft: tx < cx, ArrowRight: tx > cx, ArrowUp: ty < cy, ArrowDown: ty > cy }[event.code as "ArrowLeft"];
    if (!ok) continue;
    const d = Math.hypot(tx - cx, ty - cy);
    if (!best || d < best.d) best = { x: r.left + 8, y: r.top + 8, d };
  }
  return best && { x: best.x, y: best.y };
};

/**
 * ColumnsEditor arranges board columns and maps statuses to them; the board's configure
 * drawer and the board settings page share it. Columns reorder by dragging their handle (or
 * Space and the arrow keys) or with the previous/next buttons. Statuses move by dragging their chips
 * between columns and the "not on the board" tray, or with each column's "add status" menu.
 * `issueCounts` (issues per status id) shows what a change affects; `errors` are per column index.
 */
export function ColumnsEditor({
  columns,
  onChange,
  statuses,
  issueCounts,
  errors,
}: {
  columns: ColumnDraft[];
  onChange: (c: ColumnDraft[]) => void;
  statuses: Map<string, StatusInfo>;
  issueCounts?: Map<string, number>;
  errors?: Record<number, string>;
}) {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: keyboardCoordinates }));
  const [dragging, setDragging] = useState<string | null>(null);
  const [focusKey, setFocusKey] = useState<string | null>(null);
  const mapped = new Set(columns.flatMap((c) => c.statusIds));
  const unmapped = [...statuses.values()].filter((s) => s.inProject && !mapped.has(s.id));
  const nameOf = (id: string) => {
    if (id.startsWith(ST)) return statuses.get(id.slice(ST.length))?.name ?? "status";
    if (id === TRAY) return "Not on the board";
    const c = columns.find((x) => COL + x.key === id);
    return c ? `column ${c.name || "(unnamed)"}` : "";
  };

  const onDragEnd = (e: DragEndEvent) => {
    setDragging(null);
    if (!e.over) return;
    const active = String(e.active.id);
    const target = String(e.over.id);
    if (active.startsWith(COL)) {
      const to = columns.findIndex((c) => COL + c.key === target);
      if (to >= 0) onChange(moveColumn(columns, active.slice(COL.length), to));
      return;
    }
    onChange(assignStatus(columns, active.slice(ST.length), target === TRAY ? null : target.slice(COL.length)));
  };
  const add = () => {
    const { columns: next, key } = insertColumn(columns, columns.length, "New column");
    setFocusKey(key);
    onChange(next);
  };
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${nameOf(String(active.id))}.`,
    onDragOver: ({ active, over }) => (over ? `${nameOf(String(active.id))} is over ${nameOf(String(over.id))}.` : `${nameOf(String(active.id))} is not over a drop target.`),
    onDragEnd: ({ active, over }) => (over ? `${nameOf(String(active.id))} was dropped on ${nameOf(String(over.id))}.` : `${nameOf(String(active.id))} was dropped.`),
    onDragCancel: ({ active }) => `Moving ${nameOf(String(active.id))} was cancelled.`,
  };
  const draggedColumn = dragging?.startsWith(COL) ? columns.find((c) => COL + c.key === dragging) : undefined;
  const draggedStatus = dragging?.startsWith(ST) ? statuses.get(dragging.slice(ST.length)) : undefined;

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collision}
      accessibility={{
        announcements,
        screenReaderInstructions: { draggable: "Press Space to pick up. Use the arrow keys to move, Space to drop and Escape to cancel." },
      }}
      onDragStart={(e) => setDragging(String(e.active.id))}
      onDragEnd={onDragEnd}
      onDragCancel={() => setDragging(null)}
    >
      <div className="flex flex-col gap-3">
        <Tray statuses={unmapped} counts={issueCounts} />
        {/* Columns wrap onto more rows rather than scrolling sideways; they read left to right, top to bottom. */}
        <div className="grid grid-cols-[repeat(auto-fill,minmax(13.5rem,1fr))] gap-3">
          <SortableContext items={columns.map((c) => COL + c.key)} strategy={rectSortingStrategy}>
            {columns.map((c, i) => (
              <Column
                key={c.key}
                column={c}
                index={i}
                count={columns.length}
                columns={columns}
                statuses={statuses}
                counts={issueCounts}
                unmapped={unmapped}
                error={errors?.[i]}
                autoFocus={focusKey === c.key}
                onChange={onChange}
              />
            ))}
          </SortableContext>
          <button
            type="button"
            onClick={add}
            className="flex min-h-24 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-line-strong text-sm text-muted hover:border-accent hover:text-accent focus-visible:outline-2 focus-visible:outline-accent"
          >
            <Plus className="size-4" /> Add column
          </button>
        </div>
      </div>
      <DragOverlay dropAnimation={null}>
        {draggedStatus ? <Chip status={draggedStatus} count={issueCounts?.get(draggedStatus.id)} overlay /> : null}
        {draggedColumn ? <ColumnGhost column={draggedColumn} statuses={statuses} /> : null}
      </DragOverlay>
    </DndContext>
  );
}

function Tray({ statuses, counts }: { statuses: StatusInfo[]; counts?: Map<string, number> }) {
  const { setNodeRef, isOver } = useDroppable({ id: TRAY });
  return (
    <div
      ref={setNodeRef}
      role="group"
      aria-label="Statuses not on the board"
      className={cn("rounded-lg border border-dashed px-3 py-2.5", isOver ? "border-accent bg-accent-soft" : statuses.length ? "border-warning/50 bg-warning-soft/50" : "border-line")}
    >
      <p className="mb-1.5 flex items-center gap-1.5 text-xs font-semibold text-muted">
        {statuses.length > 0 && <TriangleAlert className="size-3.5 text-warning" />}
        Not on the board {statuses.length > 0 ? `(${statuses.length}) — issues in these statuses are hidden from the board` : "— every status the project uses has a column"}
      </p>
      <div className="flex min-h-7 flex-wrap gap-1.5">
        {statuses.map((s) => (
          <DraggableChip key={s.id} status={s} count={counts?.get(s.id)} />
        ))}
        {statuses.length === 0 && <span className="text-xs text-faint">Drag a status here to hide it.</span>}
      </div>
    </div>
  );
}

function Column({
  column,
  index,
  count,
  columns,
  statuses,
  counts,
  unmapped,
  error,
  autoFocus,
  onChange,
}: {
  column: ColumnDraft;
  index: number;
  count: number;
  columns: ColumnDraft[];
  statuses: Map<string, StatusInfo>;
  counts?: Map<string, number>;
  unmapped: StatusInfo[];
  error?: string;
  autoFocus?: boolean;
  onChange: (c: ColumnDraft[]) => void;
}) {
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging, isOver, active } = useSortable({ id: COL + column.key });
  const label = column.name.trim() || `Column ${index + 1}`;
  const statusOver = isOver && String(active?.id).startsWith(ST);
  const others = columns.filter((c) => c.key !== column.key && c.statusIds.length > 0);
  const issues = counts ? column.statusIds.reduce((n, s) => n + (counts.get(s) ?? 0), 0) : undefined;
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      role="group"
      aria-label={`Column ${label}`}
      className={cn(
        "flex min-w-0 flex-col gap-2 rounded-lg border bg-surface-2/60 p-2.5",
        statusOver ? "border-accent ring-2 ring-accent/20" : error ? "border-danger/60" : "border-line",
        isDragging && "opacity-40",
      )}
    >
      <div className="flex items-center gap-1">
        <button
          ref={setActivatorNodeRef}
          type="button"
          aria-label={`Reorder ${label}`}
          className="-ml-1 flex h-7.5 w-5 shrink-0 cursor-grab items-center justify-center rounded text-faint hover:text-ink focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
        <Input
          aria-label={`Column ${index + 1} name`}
          value={column.name}
          maxLength={60}
          autoFocus={autoFocus}
          onFocus={autoFocus ? (e) => e.currentTarget.select() : undefined}
          onChange={(e) => onChange(renameColumn(columns, column.key, e.target.value))}
          className="!h-7.5 font-medium"
          invalid={!column.name.trim() || !!error}
        />
      </div>
      {error && <p className="text-xs text-danger">{error}</p>}
      <div className="flex items-center justify-between gap-1">
        <label className="flex items-center gap-1.5 text-xs whitespace-nowrap text-muted" title="Work-in-progress limit">
          WIP
          <Input
            type="number"
            min={1}
            aria-label={`${label} WIP limit`}
            value={column.wipLimit ?? ""}
            placeholder="None"
            onChange={(e) => onChange(setWipLimit(columns, column.key, e.target.value ? Math.max(1, Number(e.target.value)) : undefined))}
            className="!h-7 !w-16 px-1.5 text-xs"
          />
        </label>
        <div className="flex">
          <IconButton label={`Move ${label} left`} size="sm" disabled={index === 0} onClick={() => onChange(moveColumn(columns, column.key, index - 1))}>
            <ChevronLeft className="size-4" />
          </IconButton>
          <IconButton label={`Move ${label} right`} size="sm" disabled={index === count - 1} onClick={() => onChange(moveColumn(columns, column.key, index + 1))}>
            <ChevronRight className="size-4" />
          </IconButton>
          <IconButton label={`Remove ${label}`} size="sm" onClick={() => onChange(removeColumn(columns, column.key))}>
            <Trash2 className="size-3.5" />
          </IconButton>
        </div>
      </div>
      <div className="flex min-h-16 flex-1 flex-col gap-1.5 rounded-md border border-dashed border-line p-1.5">
        {column.statusIds.map((id) => {
          const s = statuses.get(id);
          return s ? <DraggableChip key={id} status={s} count={counts?.get(id)} onRemove={() => onChange(assignStatus(columns, id, null))} /> : null;
        })}
        {column.statusIds.length === 0 && <p className="px-1 py-2 text-center text-xs text-faint">Drop statuses here</p>}
      </div>
      {(unmapped.length > 0 || others.length > 0) && (
        <Select aria-label={`Add a status to ${label}`} value="" onChange={(e) => e.target.value && onChange(assignStatus(columns, e.target.value, column.key))} className="!h-7 py-0 text-xs">
          <option value="">+ Add status…</option>
          {unmapped.length > 0 && (
            <optgroup label="Not on the board">
              {unmapped.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </optgroup>
          )}
          {others.map((c) => (
            <optgroup key={c.key} label={`From ${c.name.trim() || "unnamed column"}`}>
              {c.statusIds.map((id) => (
                <option key={id} value={id}>
                  {statuses.get(id)?.name ?? "Unknown status"}
                </option>
              ))}
            </optgroup>
          ))}
        </Select>
      )}
      {issues !== undefined && (
        <p className="text-[11px] text-faint">
          {issues} issue{issues === 1 ? "" : "s"} on the board
        </p>
      )}
    </div>
  );
}

function ColumnGhost({ column, statuses }: { column: ColumnDraft; statuses: Map<string, StatusInfo> }) {
  return (
    <div className="flex w-full cursor-grabbing flex-col gap-2 rounded-lg border border-line-strong bg-surface p-2.5 shadow-xl ring-2 ring-accent/30">
      <p className="flex items-center gap-1.5 truncate text-sm font-medium">
        <GripVertical className="size-4 shrink-0 text-faint" />
        {column.name || "Unnamed column"}
        {column.wipLimit && <span className="ml-auto text-xs font-normal text-muted">WIP {column.wipLimit}</span>}
      </p>
      <div className="flex flex-col items-start gap-1.5 rounded-md border border-dashed border-line p-1.5">
        {column.statusIds.map((id) => {
          const s = statuses.get(id);
          return s ? <StatusBadge key={id} status={s} /> : null;
        })}
        {column.statusIds.length === 0 && <span className="text-xs text-faint">No statuses</span>}
      </div>
    </div>
  );
}

function DraggableChip({ status, count, onRemove }: { status: StatusInfo; count?: number; onRemove?: () => void }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: ST + status.id });
  return (
    <div ref={setNodeRef} className={cn(isDragging && "opacity-40")}>
      <Chip status={status} count={count} handleProps={{ ...attributes, ...listeners }} onRemove={onRemove} />
    </div>
  );
}

function Chip({ status, count, handleProps, onRemove, overlay }: { status: StatusInfo; count?: number; handleProps?: Record<string, unknown>; onRemove?: () => void; overlay?: boolean }) {
  return (
    <div className={cn("group flex items-center gap-1 rounded-md border border-line bg-surface px-1.5 py-1", overlay && "shadow-lg ring-2 ring-accent/30")}>
      <button type="button" aria-label={`Drag ${status.name}`} className="cursor-grab rounded text-faint hover:text-ink focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing" {...handleProps}>
        <GripVertical className="size-3.5" />
      </button>
      <StatusBadge status={status} className="min-w-0" />
      {!status.inProject && (
        <span className="text-[10px] text-warning" title="No workflow in this project uses this status">
          unused
        </span>
      )}
      {count != null && count > 0 && (
        <span className="ml-auto pl-1 text-[11px] text-faint tabular-nums" title={`${count} issue${count === 1 ? "" : "s"} on the board`}>
          {count}
        </span>
      )}
      {onRemove && (
        <IconButton
          label={`Remove ${status.name} from the column`}
          size="sm"
          className={cn("size-5 text-faint opacity-60 group-focus-within:opacity-100 group-hover:opacity-100", !(count != null && count > 0) && "ml-auto")}
          onClick={onRemove}
        >
          <X className="size-3.5" />
        </IconButton>
      )}
    </div>
  );
}
