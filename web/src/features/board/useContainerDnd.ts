import { useCallback, useEffect, useRef, useState } from "react";
import {
  closestCenter,
  getFirstCollision,
  KeyboardSensor,
  MeasuringStrategy,
  PointerSensor,
  pointerWithin,
  rectIntersection,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { arrayMove, sortableKeyboardCoordinates } from "@dnd-kit/sortable";
import { findContainer, moveAcross, sameOrder, type Containers } from "./model";

export type DropResult = {
  id: string;
  from: string;
  to: string;
  /** The target container's items after the drop, including the moved one. */
  list: string[];
};

/**
 * useContainerDnd drives sortable lists that items can move between (board cells, sprints).
 * While dragging it keeps its own copy of the layout; on drop it reports where the item
 * landed and goes back to `containers`, which the caller updates optimistically.
 * `canDrop(origin, target)` restricts targets (e.g. a card stays in its swimlane).
 */
export function useContainerDnd({
  containers,
  canDrop,
  onDrop,
}: {
  containers: Containers;
  canDrop?: (from: string, to: string) => boolean;
  onDrop: (r: DropResult) => void;
}) {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [override, setOverrideState] = useState<Containers | null>(null);
  const layout = useRef<Containers | null>(null);
  const origin = useRef<{ container: string; list: string[] } | null>(null);
  const lastOverId = useRef<UniqueIdentifier | null>(null);
  const movedToNew = useRef(false);
  const items = override ?? containers;

  const setLayout = (next: Containers | null) => {
    layout.current = next;
    setOverrideState(next);
  };

  // A move between containers shifts layout; ignore collisions until it has settled.
  useEffect(() => {
    const id = requestAnimationFrame(() => (movedToNew.current = false));
    return () => cancelAnimationFrame(id);
  }, [items]);

  const allowed = useCallback(
    (target: string | undefined) => !!target && (!canDrop || !origin.current || canDrop(origin.current.container, target)),
    [canDrop],
  );

  // Pointer first, then rectangle overlap; inside a container, snap to its closest item.
  const collisionDetection: CollisionDetection = useCallback(
    (args) => {
      const droppableContainers = args.droppableContainers.filter((c) => allowed(findContainer(items, String(c.id))));
      const scoped = { ...args, droppableContainers };
      const pointer = pointerWithin(scoped);
      const hits = pointer.length > 0 ? pointer : rectIntersection(scoped);
      let overId = getFirstCollision(hits, "id");
      if (overId != null) {
        const inner = items[String(overId)];
        if (inner?.length) {
          const closest = closestCenter({ ...scoped, droppableContainers: droppableContainers.filter((c) => inner.includes(String(c.id))) });
          overId = closest[0]?.id ?? overId;
        }
        lastOverId.current = overId;
        return [{ id: overId }];
      }
      if (movedToNew.current) lastOverId.current = activeId;
      return lastOverId.current != null ? [{ id: lastOverId.current }] : [];
    },
    [items, allowed, activeId],
  );

  const onDragStart = ({ active }: DragStartEvent) => {
    const id = String(active.id);
    const container = findContainer(containers, id);
    if (!container) return;
    origin.current = { container, list: containers[container] };
    setActiveId(id);
    setLayout(containers);
  };

  const onDragOver = ({ active, over }: DragOverEvent) => {
    const cur = layout.current;
    if (!over || !cur) return;
    const id = String(active.id);
    const overId = String(over.id);
    const from = findContainer(cur, id);
    const to = findContainer(cur, overId);
    if (!from || !to || from === to || !allowed(to)) return;
    let index = cur[to].length;
    if (!(overId in cur)) {
      // Entering another list over an item takes that item's place (as sorting does), unless
      // the card is wholly below it; the preview always shows where the drop will land.
      const translated = active.rect.current.translated;
      const below = !!translated && translated.top > over.rect.top + over.rect.height;
      index = cur[to].indexOf(overId) + (below ? 1 : 0);
    }
    movedToNew.current = true;
    setLayout(moveAcross(cur, id, from, to, index));
  };

  const reset = () => {
    setActiveId(null);
    setLayout(null);
    lastOverId.current = null;
  };

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    const cur = layout.current;
    const start = origin.current;
    const id = String(active.id);
    origin.current = null;
    if (!cur || !start || !over) return reset();
    const to = findContainer(cur, id);
    if (!to || !allowed(to)) return reset();
    let list = cur[to];
    const overId = String(over.id);
    if (!(overId in cur) && findContainer(cur, overId) === to) {
      const a = list.indexOf(id);
      const b = list.indexOf(overId);
      if (a !== b) list = arrayMove(list, a, b);
    }
    // Report before resetting so the caller's optimistic update lands in the same render.
    if (to !== start.container || !sameOrder(list, start.list)) onDrop({ id, from: start.container, to, list });
    reset();
  };

  const onDragCancel = () => {
    origin.current = null;
    reset();
  };

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates, keyboardCodes: { start: ["Space"], cancel: ["Escape"], end: ["Space", "Enter"] } }),
  );

  return {
    items,
    activeId,
    contextProps: {
      sensors,
      collisionDetection,
      measuring: { droppable: { strategy: MeasuringStrategy.Always } },
      onDragStart,
      onDragOver,
      onDragEnd,
      onDragCancel,
    },
  };
}
