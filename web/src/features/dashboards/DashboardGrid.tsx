import { useMemo } from "react";
import { closestCenter, DndContext, KeyboardSensor, PointerSensor, pointerWithin, useSensor, useSensors, type Announcements, type CollisionDetection, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { ArrowLeft, ArrowRight, Check, CopyPlus, Ellipsis, GripVertical, Settings2, Trash2 } from "lucide-react";
import { IconButton, MenuItem, MenuLabel, Popover } from "@/components/ui";
import { cn } from "@/lib/cn";
import { GadgetContext } from "./GadgetFrame";
import { gadgetMeta, gadgetTitle, moveItem, duplicateGadget, type Gadget } from "./model";
import { BreakdownGadget, CreatedResolvedGadget, CycleTimeGadget, FlowGadget, IssueListGadget, StatGadget, UnknownGadget } from "./QueryGadgets";
import { SprintChartGadget, SprintHealthGadget, VelocityGadget } from "./SprintGadgets";

// Widths are columns of three; at two columns a wide gadget fills the row, at one all stack.
const SPAN: Record<number, string> = { 1: "", 2: "@3xl:col-span-2", 3: "@3xl:col-span-2 @5xl:col-span-3" };
// Gadgets differ in size, so the others stay put while one is dragged (a shifting preview
// assumes equal sizes) and the gadget it would take the place of is outlined instead.
const noShift = () => null;
// The gadget under the pointer; for the keyboard, which has no pointer, the nearest one.
const collision: CollisionDetection = (args) => {
  const within = pointerWithin(args);
  return within.length > 0 ? within : closestCenter(args);
};

const WIDTHS: { value: 1 | 2 | 3; label: string }[] = [
  { value: 1, label: "Narrow (one column)" },
  { value: 2, label: "Wide (two columns)" },
  { value: 3, label: "Full width" },
];

function GadgetView({ gadget }: { gadget: Gadget }) {
  switch (gadget.type) {
    case "sprint_burndown":
      return <SprintChartGadget kind="burndown" />;
    case "sprint_burnup":
      return <SprintChartGadget kind="burnup" />;
    case "sprint_health":
      return <SprintHealthGadget />;
    case "velocity":
      return <VelocityGadget />;
    case "cumulative_flow":
      return <FlowGadget />;
    case "created_resolved":
      return <CreatedResolvedGadget />;
    case "cycle_time":
      return <CycleTimeGadget />;
    case "breakdown":
      return <BreakdownGadget />;
    case "stat":
      return <StatGadget />;
    case "issue_list":
      return <IssueListGadget />;
    default:
      return <UnknownGadget />;
  }
}

/**
 * DashboardGrid lays gadgets out on a responsive three-column grid. In edit mode each gadget
 * gets a drag handle (pointer or keyboard: Space to lift, arrows to move, Space to drop) and a
 * menu to configure, resize, move, duplicate or remove it.
 */
export function DashboardGrid({
  gadgets,
  editing,
  onChange,
  onConfigure,
  openIssue,
}: {
  gadgets: Gadget[];
  editing: boolean;
  onChange?: (g: Gadget[]) => void;
  onConfigure?: (id: string) => void;
  openIssue: (key: string) => void;
}) {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  const ids = useMemo(() => gadgets.map((g) => g.id), [gadgets]);
  const titleOf = (id: string | number) => {
    const g = gadgets.find((x) => x.id === id);
    return g ? gadgetTitle(g) : "gadget";
  };
  const position = (id: string | number) => `position ${ids.indexOf(String(id)) + 1} of ${ids.length}`;
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${titleOf(active.id)} at ${position(active.id)}.`,
    onDragOver: ({ active, over }) => (over ? `${titleOf(active.id)} is over ${position(over.id)}.` : `${titleOf(active.id)} is not over a position.`),
    onDragEnd: ({ active, over }) => (over ? `${titleOf(active.id)} was dropped at ${position(over.id)}.` : `${titleOf(active.id)} was dropped.`),
    onDragCancel: ({ active }) => `Moving ${titleOf(active.id)} was cancelled.`,
  };
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    onChange?.(moveItem(gadgets, ids.indexOf(String(active.id)), ids.indexOf(String(over.id))));
  };

  const grid = (
    <div className="grid grid-cols-1 gap-4 @3xl:grid-cols-2 @5xl:grid-cols-3">
      {gadgets.map((g, i) =>
        editing ? (
          <EditableGadget
            key={g.id}
            gadget={g}
            index={i}
            count={gadgets.length}
            openIssue={openIssue}
            onConfigure={() => onConfigure?.(g.id)}
            onUpdate={(next) => onChange?.(gadgets.map((x) => (x.id === g.id ? next : x)))}
            onMove={(to) => onChange?.(moveItem(gadgets, i, to))}
            onDuplicate={() => onChange?.(duplicateGadget(gadgets, g.id))}
            onRemove={() => onChange?.(gadgets.filter((x) => x.id !== g.id))}
          />
        ) : (
          <div key={g.id} className={cn("min-w-0", SPAN[g.width])}>
            <GadgetContext.Provider value={{ gadget: g, openIssue }}>
              <GadgetView gadget={g} />
            </GadgetContext.Provider>
          </div>
        ),
      )}
    </div>
  );
  if (!editing) return <div className="@container">{grid}</div>;
  return (
    <div className="@container">
      <DndContext
        sensors={sensors}
        collisionDetection={collision}
        onDragEnd={onDragEnd}
        accessibility={{
          announcements,
          screenReaderInstructions: { draggable: "To move a gadget, press Space or Enter on its handle, use the arrow keys to move it, then Space or Enter to drop it, or Escape to cancel." },
        }}
      >
        <SortableContext items={ids} strategy={noShift}>
          {grid}
        </SortableContext>
      </DndContext>
    </div>
  );
}

function EditableGadget({
  gadget,
  index,
  count,
  openIssue,
  onConfigure,
  onUpdate,
  onMove,
  onDuplicate,
  onRemove,
}: {
  gadget: Gadget;
  index: number;
  count: number;
  openIssue: (key: string) => void;
  onConfigure: () => void;
  onUpdate: (g: Gadget) => void;
  onMove: (to: number) => void;
  onDuplicate: () => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging, isOver } = useSortable({ id: gadget.id });
  const title = gadgetTitle(gadget);
  const handle = (
    <button
      ref={setActivatorNodeRef}
      type="button"
      {...attributes}
      {...listeners}
      aria-label={`Move ${title}`}
      title="Drag to move"
      className="-ml-2 inline-flex size-7 shrink-0 cursor-grab items-center justify-center rounded-md text-faint hover:bg-surface-2 hover:text-ink focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing"
    >
      <GripVertical className="size-4" />
    </button>
  );
  const controls = (
    <div className="flex shrink-0 items-center">
      <IconButton size="sm" label={`Configure ${title}`} onClick={onConfigure}>
        <Settings2 className="size-4" />
      </IconButton>
      <Popover
        align="end"
        trigger={({ toggle, open }) => (
          <IconButton size="sm" label={`More actions for ${title}`} aria-haspopup="menu" aria-expanded={open} onClick={toggle}>
            <Ellipsis className="size-4" />
          </IconButton>
        )}
        content={(close) => (
          <div role="menu" className="w-56">
            <MenuLabel>Width</MenuLabel>
            {WIDTHS.map((w) => (
              <MenuItem key={w.value} icon={<Check className={cn("size-4", gadget.width === w.value ? "text-accent" : "invisible")} />} onClick={() => (close(), onUpdate({ ...gadget, width: w.value }))}>
                {w.label}
              </MenuItem>
            ))}
            <div className="my-1 border-t border-line" />
            <MenuItem icon={<ArrowLeft className="size-4" />} disabled={index === 0} onClick={() => (close(), onMove(index - 1))}>
              Move earlier
            </MenuItem>
            <MenuItem icon={<ArrowRight className="size-4" />} disabled={index === count - 1} onClick={() => (close(), onMove(index + 1))}>
              Move later
            </MenuItem>
            <MenuItem icon={<CopyPlus className="size-4" />} onClick={() => (close(), onDuplicate())}>
              Duplicate
            </MenuItem>
            <MenuItem icon={<Trash2 className="size-4" />} danger onClick={() => (close(), onRemove())}>
              Remove
            </MenuItem>
          </div>
        )}
      />
    </div>
  );
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn("min-w-0 rounded-lg", SPAN[gadget.width], isDragging ? "relative z-10 opacity-80 shadow-xl ring-2 ring-accent" : isOver && "ring-2 ring-accent/50 ring-offset-2 ring-offset-canvas")}
      data-gadget-type={gadgetMeta(gadget.type).type}
    >
      <GadgetContext.Provider value={{ gadget, openIssue, handle, controls }}>
        <GadgetView gadget={gadget} />
      </GadgetContext.Provider>
    </div>
  );
}
