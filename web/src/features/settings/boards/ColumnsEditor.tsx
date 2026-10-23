import { useState } from "react";
import { DndContext, DragOverlay, KeyboardSensor, PointerSensor, useDraggable, useDroppable, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { ChevronLeft, ChevronRight, GripVertical, Plus, Trash2, TriangleAlert, X } from "lucide-react";
import { IconButton, Input, Select } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import type { StatusCategory } from "@/api/types";
import { cn } from "@/lib/cn";

export type ColumnDraft = { key: string; name: string; statusIds: string[]; wipLimit?: number };
export type StatusInfo = { id: string; name: string; category: StatusCategory; inProject: boolean };

let seq = 0;
export const newColumnKey = () => `col-${++seq}`;

/**
 * ColumnsEditor arranges board columns and assigns statuses to them. Statuses can be dragged
 * between columns and the "not on the board" tray, or assigned with the per-column menu.
 */
export function ColumnsEditor({ columns, onChange, statuses }: { columns: ColumnDraft[]; onChange: (c: ColumnDraft[]) => void; statuses: Map<string, StatusInfo> }) {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor));
  const [dragging, setDragging] = useState<string | null>(null);
  const mapped = new Set(columns.flatMap((c) => c.statusIds));
  const unmapped = [...statuses.values()].filter((s) => s.inProject && !mapped.has(s.id));

  const moveStatus = (statusId: string, to: string | null) => {
    onChange(
      columns.map((c) => {
        const without = c.statusIds.filter((id) => id !== statusId);
        return c.key === to ? { ...c, statusIds: [...without, statusId] } : { ...c, statusIds: without };
      }),
    );
  };
  const onDragEnd = (e: DragEndEvent) => {
    setDragging(null);
    if (!e.over) return;
    const statusId = String(e.active.id).replace(/^st:/, "");
    const target = String(e.over.id);
    moveStatus(statusId, target === "unmapped" ? null : target.replace(/^col:/, ""));
  };
  const update = (key: string, patch: Partial<ColumnDraft>) => onChange(columns.map((c) => (c.key === key ? { ...c, ...patch } : c)));
  const move = (i: number, d: -1 | 1) => {
    const next = [...columns];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    onChange(next);
  };

  return (
    <DndContext sensors={sensors} onDragStart={(e) => setDragging(String(e.active.id).replace(/^st:/, ""))} onDragEnd={onDragEnd} onDragCancel={() => setDragging(null)}>
      <div className="flex flex-col gap-3">
        <Tray statuses={unmapped} />
        <div className="flex gap-3 overflow-x-auto pb-2">
          {columns.map((c, i) => (
            <Column
              key={c.key}
              column={c}
              index={i}
              count={columns.length}
              statuses={statuses}
              assignable={unmapped}
              onUpdate={(p) => update(c.key, p)}
              onMove={(d) => move(i, d)}
              onRemove={() => onChange(columns.filter((x) => x.key !== c.key))}
              onAssign={(id) => moveStatus(id, c.key)}
              onUnassign={(id) => moveStatus(id, null)}
            />
          ))}
          <button
            type="button"
            onClick={() => onChange([...columns, { key: newColumnKey(), name: "New column", statusIds: [] }])}
            className="flex w-44 shrink-0 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-line-strong text-sm text-muted hover:border-accent hover:text-accent"
          >
            <Plus className="size-4" /> Add column
          </button>
        </div>
      </div>
      <DragOverlay dropAnimation={null}>{dragging && statuses.get(dragging) ? <Chip status={statuses.get(dragging)!} overlay /> : null}</DragOverlay>
    </DndContext>
  );
}

function Tray({ statuses }: { statuses: StatusInfo[] }) {
  const { setNodeRef, isOver } = useDroppable({ id: "unmapped" });
  return (
    <div
      ref={setNodeRef}
      className={cn("rounded-lg border border-dashed px-3 py-2.5", isOver ? "border-accent bg-accent-soft" : statuses.length ? "border-warning/50 bg-warning-soft/50" : "border-line")}
    >
      <p className="mb-1.5 flex items-center gap-1.5 text-xs font-semibold text-muted">
        {statuses.length > 0 && <TriangleAlert className="size-3.5 text-warning" />}
        Not on the board {statuses.length > 0 ? `(${statuses.length}) — issues in these statuses are hidden from the board` : "— every status the project uses has a column"}
      </p>
      <div className="flex min-h-7 flex-wrap gap-1.5">
        {statuses.map((s) => (
          <DraggableChip key={s.id} status={s} />
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
  statuses,
  assignable,
  onUpdate,
  onMove,
  onRemove,
  onAssign,
  onUnassign,
}: {
  column: ColumnDraft;
  index: number;
  count: number;
  statuses: Map<string, StatusInfo>;
  assignable: StatusInfo[];
  onUpdate: (p: Partial<ColumnDraft>) => void;
  onMove: (d: -1 | 1) => void;
  onRemove: () => void;
  onAssign: (id: string) => void;
  onUnassign: (id: string) => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `col:${column.key}` });
  const label = column.name || `Column ${index + 1}`;
  return (
    <div ref={setNodeRef} className={cn("flex w-64 shrink-0 flex-col gap-2 rounded-lg border bg-surface-2/60 p-2.5", isOver ? "border-accent ring-2 ring-accent/20" : "border-line")}>
      <div className="flex items-center gap-1">
        <Input
          aria-label={`Column ${index + 1} name`}
          value={column.name}
          maxLength={60}
          onChange={(e) => onUpdate({ name: e.target.value })}
          className="!h-7.5 font-medium"
          invalid={!column.name.trim()}
        />
      </div>
      <div className="flex items-center justify-between gap-1">
        <label className="flex items-center gap-1.5 text-xs whitespace-nowrap text-muted" title="Work-in-progress limit">
          WIP
          <Input
            type="number"
            min={1}
            aria-label={`${label} WIP limit`}
            value={column.wipLimit ?? ""}
            placeholder="None"
            onChange={(e) => onUpdate({ wipLimit: e.target.value ? Math.max(1, Number(e.target.value)) : undefined })}
            className="!h-7 !w-16 px-1.5 text-xs"
          />
        </label>
        <div className="flex">
          <IconButton label={`Move ${label} left`} size="sm" disabled={index === 0} onClick={() => onMove(-1)}>
            <ChevronLeft className="size-4" />
          </IconButton>
          <IconButton label={`Move ${label} right`} size="sm" disabled={index === count - 1} onClick={() => onMove(1)}>
            <ChevronRight className="size-4" />
          </IconButton>
          <IconButton label={`Remove ${label}`} size="sm" onClick={onRemove}>
            <Trash2 className="size-3.5" />
          </IconButton>
        </div>
      </div>
      <div className="flex min-h-16 flex-col gap-1.5 rounded-md border border-dashed border-line p-1.5">
        {column.statusIds.map((id) => {
          const s = statuses.get(id);
          return s ? <DraggableChip key={id} status={s} onRemove={() => onUnassign(id)} /> : null;
        })}
        {column.statusIds.length === 0 && <p className="px-1 py-2 text-center text-xs text-faint">Drop statuses here</p>}
      </div>
      {assignable.length > 0 && (
        <Select aria-label={`Add a status to ${label}`} value="" onChange={(e) => e.target.value && onAssign(e.target.value)} className="!h-7 py-0 text-xs">
          <option value="">+ Add status…</option>
          {assignable.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </Select>
      )}
    </div>
  );
}

function DraggableChip({ status, onRemove }: { status: StatusInfo; onRemove?: () => void }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: `st:${status.id}` });
  return (
    <div ref={setNodeRef} className={cn(isDragging && "opacity-40")}>
      <Chip status={status} handleProps={{ ...attributes, ...listeners }} onRemove={onRemove} />
    </div>
  );
}

function Chip({ status, handleProps, onRemove, overlay }: { status: StatusInfo; handleProps?: Record<string, unknown>; onRemove?: () => void; overlay?: boolean }) {
  return (
    <div className={cn("group flex items-center gap-1 rounded-md border border-line bg-surface px-1.5 py-1", overlay && "shadow-lg ring-2 ring-accent/30")}>
      <button type="button" aria-label={`Drag ${status.name}`} className="cursor-grab text-faint hover:text-ink active:cursor-grabbing" {...handleProps}>
        <GripVertical className="size-3.5" />
      </button>
      <StatusBadge status={status} className="min-w-0" />
      {!status.inProject && (
        <span className="text-[10px] text-warning" title="No workflow in this project uses this status">
          unused
        </span>
      )}
      {onRemove && (
        <IconButton
          label={`Remove ${status.name} from the column`}
          size="sm"
          className="ml-auto size-5 text-faint opacity-60 group-focus-within:opacity-100 group-hover:opacity-100"
          onClick={onRemove}
        >
          <X className="size-3.5" />
        </IconButton>
      )}
    </div>
  );
}
