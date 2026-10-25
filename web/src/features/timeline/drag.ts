import type { Issue } from "@/api/types";
import type { DragMode } from "./Bar";
import type { Schedule } from "./model";

/** A drag in progress: moving or resizing a bar, drawing a new range, or drawing a dependency. */
export type BarDrag = { kind: "bar"; mode: DragMode; issue: Issue; base: Schedule; clientX0: number; scroll0: number; delta: number; moved: boolean; editable: boolean };
export type CreateDrag = { kind: "create"; issue: Issue; anchor: number; current: number };
export type LinkDrag = { kind: "link"; from: string; x0: number; y0: number; x: number; y: number; target?: string };
export type Drag = BarDrag | CreateDrag | LinkDrag;

/** preview applies a bar drag to its schedule: moving shifts both ends, resizing keeps start ≤ end. */
export function preview(d: BarDrag): { start: number; end: number } {
  const { start, end } = d.base;
  if (d.mode === "move") return { start: start + d.delta, end: end + d.delta };
  if (d.mode === "start") return { start: Math.min(start + d.delta, end), end };
  return { start, end: Math.max(end + d.delta, start) };
}
