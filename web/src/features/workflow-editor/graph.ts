import type { Category, Position, WorkflowDoc } from "./model";
import { withLayout } from "./model";

/*
 * Graph view of a workflow document: one node per status, one edge per (transition, source
 * status) pair. A transition with several `from` statuses becomes several edges; a global
 * transition (no `from`) has no edge and shows as a "from any" badge on its target node.
 */

export interface GlobalBadge {
  index: number;
  name: string;
}

export interface StatusNodeData extends Record<string, unknown> {
  name: string;
  category: Category;
  initial: boolean;
  globals: GlobalBadge[];
  errors: string[];
  readOnly: boolean;
}

export interface TransitionEdgeData extends Record<string, unknown> {
  index: number;
  from: string;
  name: string;
  /** Perpendicular offset (px) separating parallel and opposite edges between the same pair. */
  offset: number;
  hasCondition: boolean;
  validators: number;
  screen: number;
  postFunctions: number;
  error: boolean;
}

export interface GraphNode {
  id: string;
  position: Position;
  data: StatusNodeData;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  data: TransitionEdgeData;
}

export const PARALLEL_SPACING = 34;

export function edgeId(index: number, from: string) {
  return `t${index}|${from}`;
}

/** parseEdgeId reverses edgeId. */
export function parseEdgeId(id: string): { index: number; from: string } | null {
  const m = /^t(\d+)\|(.*)$/.exec(id);
  return m ? { index: Number(m[1]), from: m[2] } : null;
}

export function buildGraph(input: WorkflowDoc, opts: { statusErrors?: Map<number, string[]>; transitionErrors?: Set<number>; readOnly?: boolean } = {}): { nodes: GraphNode[]; edges: GraphEdge[] } {
  const doc = withLayout(input);
  const canonical = (n: string) => doc.statuses.find((s) => s.name.toLowerCase() === n.trim().toLowerCase())?.name;

  const globals = new Map<string, GlobalBadge[]>();
  const edges: GraphEdge[] = [];
  doc.transitions.forEach((t, index) => {
    const target = canonical(t.to);
    if (!target) return;
    if (!t.from) {
      const list = globals.get(target) ?? [];
      list.push({ index, name: t.name });
      globals.set(target, list);
      return;
    }
    for (const f of t.from) {
      const source = canonical(f);
      if (!source || source === target) continue;
      edges.push({
        id: edgeId(index, source),
        source,
        target,
        data: {
          index,
          from: source,
          name: t.name,
          offset: 0,
          hasCondition: !!t.condition?.trim(),
          validators: t.validators?.length ?? 0,
          screen: t.screen?.length ?? 0,
          postFunctions: t.postFunctions?.length ?? 0,
          error: opts.transitionErrors?.has(index) ?? false,
        },
      });
    }
  });
  assignOffsets(edges);

  const nodes: GraphNode[] = doc.statuses.map((s, i) => ({
    id: s.name,
    position: doc.layout![s.name],
    data: {
      name: s.name,
      category: s.category,
      initial: !!s.initial || (i === 0 && !doc.statuses.some((x) => x.initial)),
      globals: globals.get(s.name) ?? [],
      errors: opts.statusErrors?.get(i) ?? [],
      readOnly: !!opts.readOnly,
    },
  }));
  return { nodes, edges };
}

/**
 * assignOffsets spreads edges that connect the same two statuses. Edges in one direction only
 * are centred around the straight line; when both directions exist each direction bends to
 * its own side (the perpendicular flips with the direction, so equal offsets separate them).
 */
export function assignOffsets(edges: { source: string; target: string; data: { offset: number } }[]) {
  const groups = new Map<string, typeof edges>();
  for (const e of edges) {
    const key = [e.source, e.target].sort().join("\u0000");
    const g = groups.get(key) ?? [];
    g.push(e);
    groups.set(key, g);
  }
  for (const g of groups.values()) {
    const a = g[0].source;
    const fwd = g.filter((e) => e.source === a);
    const back = g.filter((e) => e.source !== a);
    if (back.length === 0) {
      fwd.forEach((e, k) => (e.data.offset = (k - (fwd.length - 1) / 2) * PARALLEL_SPACING));
    } else {
      fwd.forEach((e, k) => (e.data.offset = (k + 0.6) * PARALLEL_SPACING));
      back.forEach((e, k) => (e.data.offset = (k + 0.6) * PARALLEL_SPACING));
    }
  }
}

// ---------------------------------------------------------------------------
// Geometry for floating edges

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** borderPoint is where the ray from the rectangle's centre towards `p` leaves the rectangle. */
export function borderPoint(r: Rect, p: Position, pad = 0): Position {
  const cx = r.x + r.width / 2;
  const cy = r.y + r.height / 2;
  const dx = p.x - cx;
  const dy = p.y - cy;
  if (dx === 0 && dy === 0) return { x: cx, y: cy };
  const hw = r.width / 2 + pad;
  const hh = r.height / 2 + pad;
  const scale = Math.min(dx === 0 ? Infinity : hw / Math.abs(dx), dy === 0 ? Infinity : hh / Math.abs(dy));
  return { x: cx + dx * scale, y: cy + dy * scale };
}

function curve(src: Rect, dst: Rect, offset: number) {
  const s = { x: src.x + src.width / 2, y: src.y + src.height / 2 };
  const t = { x: dst.x + dst.width / 2, y: dst.y + dst.height / 2 };
  const dx = t.x - s.x;
  const dy = t.y - s.y;
  const len = Math.hypot(dx, dy) || 1;
  // A quadratic curve reaches half of its control point's offset at t = 0.5.
  const c = { x: (s.x + t.x) / 2 + (-dy / len) * offset * 2, y: (s.y + t.y) / 2 + (dx / len) * offset * 2 };
  return { start: borderPoint(src, c, 1), c, end: borderPoint(dst, c, 4) };
}

/**
 * edgePath draws a quadratic curve between two node rectangles, bent by `offset` pixels at its
 * midpoint, and returns the label position on the curve.
 */
export function edgePath(src: Rect, dst: Rect, offset: number): { path: string; labelX: number; labelY: number } {
  const { start, c, end } = curve(src, dst, offset);
  const labelX = 0.25 * start.x + 0.5 * c.x + 0.25 * end.x;
  const labelY = 0.25 * start.y + 0.5 * c.y + 0.25 * end.y;
  const r = (n: number) => Math.round(n * 10) / 10;
  return { path: `M ${r(start.x)},${r(start.y)} Q ${r(c.x)},${r(c.y)} ${r(end.x)},${r(end.y)}`, labelX, labelY };
}

function inside(p: Position, r: Rect) {
  return p.x > r.x && p.x < r.x + r.width && p.y > r.y && p.y < r.y + r.height;
}

/** curveHits reports whether the edge's curve passes through any of `obstacles`. */
export function curveHits(src: Rect, dst: Rect, offset: number, obstacles: Rect[]): boolean {
  const { start, c, end } = curve(src, dst, offset);
  for (let i = 1; i < 40; i++) {
    const t = i / 40;
    const p = {
      x: (1 - t) * (1 - t) * start.x + 2 * (1 - t) * t * c.x + t * t * end.x,
      y: (1 - t) * (1 - t) * start.y + 2 * (1 - t) * t * c.y + t * t * end.y,
    };
    if (obstacles.some((r) => inside(p, r))) return true;
  }
  return false;
}

const ROUTE_STEP = 26;
const ROUTE_MARGIN = 14;

/**
 * routeAroundNodes bends edges that would cross other statuses (e.g. "Reopen" from the last
 * status back to the first one) until they clear them, keeping the parallel-edge spacing.
 */
export function routeAroundNodes(edges: { source: string; target: string; data: { offset: number } }[], rects: Map<string, Rect>) {
  for (const e of edges) {
    const s = rects.get(e.source);
    const t = rects.get(e.target);
    if (!s || !t) continue;
    const obstacles = [...rects.entries()]
      .filter(([id]) => id !== e.source && id !== e.target)
      .map(([, r]) => ({ x: r.x - ROUTE_MARGIN, y: r.y - ROUTE_MARGIN, width: r.width + 2 * ROUTE_MARGIN, height: r.height + 2 * ROUTE_MARGIN }));
    const base = e.data.offset;
    if (!obstacles.length || !curveHits(s, t, base, obstacles)) continue;
    const sign = base < 0 ? -1 : 1;
    for (let k = 1; k <= 12; k++) {
      const off = base + sign * k * ROUTE_STEP;
      if (!curveHits(s, t, off, obstacles)) {
        e.data.offset = off;
        break;
      }
      if (base === 0 && !curveHits(s, t, -off, obstacles)) {
        e.data.offset = -off;
        break;
      }
    }
  }
}
