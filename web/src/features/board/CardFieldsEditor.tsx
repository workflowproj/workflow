import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowDown, ArrowUp, GripVertical } from "lucide-react";
import type { Issue } from "@/api/types";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { CARD_FIELDS, localDate, moveCardField, toggleCardField, type CardField } from "./card-fields";
import { IssueCard } from "./IssueCard";

const meta = new Map(CARD_FIELDS.map((f) => [f.field, f]));

/**
 * CardFieldsEditor picks what board cards show besides the summary, type and key, and in what
 * order (drag the handles, or use the arrows), next to a live preview card.
 */
export function CardFieldsEditor({ value, onChange, projectKey = "PRJ", error }: { value: CardField[]; onChange: (f: CardField[]) => void; projectKey?: string; error?: string }) {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  const hidden = CARD_FIELDS.filter((f) => !value.includes(f.field));
  const onDragEnd = (e: DragEndEvent) => {
    if (!e.over || e.active.id === e.over.id) return;
    onChange(moveCardField(value, e.active.id as CardField, value.indexOf(e.over.id as CardField)));
  };
  return (
    <div className="grid gap-5 md:grid-cols-[minmax(0,1fr)_16rem]">
      <div className="min-w-0">
        <p className="mb-2 text-xs text-muted">Cards always show the summary, issue type and key. Epic and labels sit under the summary; the rest line up after the key.</p>
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={value} strategy={verticalListSortingStrategy}>
            <ul aria-label="Fields on cards" className="flex flex-col rounded-md border border-line">
              {value.map((f, i) => (
                <ShownRow key={f} field={f} index={i} count={value.length} onToggle={() => onChange(toggleCardField(value, f, false))} onMove={(to) => onChange(moveCardField(value, f, to))} />
              ))}
              {value.length === 0 && <li className="px-3 py-2.5 text-sm text-faint">Cards show only the summary, type and key.</li>}
            </ul>
          </SortableContext>
        </DndContext>
        {hidden.length > 0 && (
          <>
            <p className="mt-3 mb-1.5 text-[11px] font-semibold tracking-wide text-faint uppercase">Hidden</p>
            <ul aria-label="Fields not on cards" className="flex flex-col rounded-md border border-dashed border-line">
              {hidden.map((f) => (
                <li key={f.field} className="flex items-center gap-2 border-b border-line px-2 py-1.5 last:border-b-0">
                  <span className="w-5 shrink-0" />
                  <FieldCheckbox field={f.field} checked={false} onChange={() => onChange(toggleCardField(value, f.field, true))} />
                </li>
              ))}
            </ul>
          </>
        )}
        {error && <p className="mt-2 text-sm text-danger">{error}</p>}
      </div>
      <div>
        <p className="mb-2 text-[11px] font-semibold tracking-wide text-faint uppercase">Preview</p>
        <div className="rounded-lg bg-surface-2 p-1.5" aria-label="Card preview" role="group">
          <IssueCard issue={sampleIssue(projectKey)} fields={value} />
        </div>
      </div>
    </div>
  );
}

function ShownRow({ field, index, count, onToggle, onMove }: { field: CardField; index: number; count: number; onToggle: () => void; onMove: (to: number) => void }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: field });
  const label = meta.get(field)?.label ?? field;
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn("relative flex items-center gap-2 border-b border-line bg-surface px-2 py-1.5 last:border-b-0", isDragging && "z-10 shadow-lg ring-1 ring-accent/30")}
    >
      <button
        ref={setActivatorNodeRef}
        type="button"
        aria-label={`Reorder ${label}`}
        className="flex w-5 shrink-0 cursor-grab justify-center rounded text-faint hover:text-ink focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing"
        {...attributes}
        {...listeners}
      >
        <GripVertical className="size-4" />
      </button>
      <FieldCheckbox field={field} checked onChange={onToggle} />
      <span className="flex shrink-0">
        <IconButton label={`Move ${label} up`} size="sm" className="size-6" disabled={index === 0} onClick={() => onMove(index - 1)}>
          <ArrowUp className="size-3.5" />
        </IconButton>
        <IconButton label={`Move ${label} down`} size="sm" className="size-6" disabled={index === count - 1} onClick={() => onMove(index + 1)}>
          <ArrowDown className="size-3.5" />
        </IconButton>
      </span>
    </li>
  );
}

function FieldCheckbox({ field, checked, onChange }: { field: CardField; checked: boolean; onChange: () => void }) {
  const m = meta.get(field);
  return (
    <label className="flex min-w-0 flex-1 cursor-pointer items-baseline gap-2 text-sm">
      <input type="checkbox" checked={checked} onChange={onChange} className="size-4 shrink-0 translate-y-0.5 rounded border-line-strong accent-accent" />
      <span className={cn("shrink-0", !checked && "text-muted")}>{m?.label ?? field}</span>
      <span className="truncate text-xs text-faint">{m?.hint}</span>
    </label>
  );
}

const DAY = 86_400_000;

/** sampleIssue fills every optional field so the preview shows what each one looks like. */
function sampleIssue(projectKey: string): Issue {
  const status = { id: "preview-status", name: "In Progress", category: "in_progress" as const };
  return {
    id: "preview",
    key: `${projectKey}-42`,
    projectKey,
    type: { id: "preview-story", key: "story", name: "Story", icon: "story", color: "#22a06b", level: 0, kind: "work" },
    summary: "Retry failed webhook deliveries with backoff",
    status,
    priority: "high",
    assignee: { id: "preview-user", displayName: "Ada Lovelace", email: "ada@example.com", kind: "human" },
    parent: { id: "preview-epic", key: `${projectKey}-7`, summary: "Reliable integrations", type: { id: "preview-epic-type", key: "epic", name: "Epic", icon: "epic", color: "#8b5cf6", level: 1, kind: "work" }, status },
    storyPoints: 3,
    dueDate: localDate(new Date(Date.now() + 4 * DAY)),
    labels: ["backend", "webhooks"],
    rank: "0",
    version: 1,
    fields: {},
    dev: { branches: 1, commits: 4, pullRequests: 1, openPullRequests: 1, mergedPullRequests: 0, pipelineState: "success" },
    timeTracking: { originalSeconds: 8 * 3600, remainingSeconds: 5 * 3600, spentSeconds: 3 * 3600 },
    statusChangedAt: new Date(Date.now() - 3 * DAY - 3_600_000).toISOString(),
    createdAt: new Date(Date.now() - 9 * DAY).toISOString(),
    updatedAt: new Date().toISOString(),
  };
}
