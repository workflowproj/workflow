import { describe, expect, it } from "vitest";
import { assignOffsets, borderPoint, buildGraph, curveHits, edgeId, edgePath, parseEdgeId, PARALLEL_SPACING, routeAroundNodes } from "./graph";
import type { WorkflowDoc } from "./model";

const doc: WorkflowDoc = {
  name: "W",
  statuses: [
    { name: "Open", category: "todo" },
    { name: "Doing", category: "in_progress" },
    { name: "Done", category: "done" },
  ],
  transitions: [
    { name: "Start", from: ["Open"], to: "Doing" },
    { name: "Stop", from: ["Doing"], to: "Open" },
    { name: "Finish", from: ["Open", "doing"], to: "Done", condition: "true", postFunctions: [{ notify: "watchers" }] },
    { name: "Reset", to: "Open" },
    { name: "Broken", from: ["Nowhere"], to: "Done" },
  ],
  layout: { Open: { x: 0, y: 0 } },
};

describe("buildGraph", () => {
  const g = buildGraph(doc, { transitionErrors: new Set([2]), statusErrors: new Map([[1, ["bad"]]]) });

  it("creates one node per status with a position for each", () => {
    expect(g.nodes.map((n) => n.id)).toEqual(["Open", "Doing", "Done"]);
    expect(g.nodes.every((n) => Number.isFinite(n.position.x) && Number.isFinite(n.position.y))).toBe(true);
    expect(g.nodes[0].data.initial).toBe(true); // first status when none is marked
    expect(g.nodes[1].data.errors).toEqual(["bad"]);
  });

  it("creates one edge per source status, matching names case-insensitively", () => {
    const finish = g.edges.filter((e) => e.data.index === 2);
    expect(finish.map((e) => [e.source, e.target])).toEqual([
      ["Open", "Done"],
      ["Doing", "Done"],
    ]);
    expect(finish[0].data).toMatchObject({ hasCondition: true, postFunctions: 1, error: true });
    expect(g.edges.some((e) => e.data.index === 4)).toBe(false); // unknown source: no edge
  });

  it("shows global transitions as badges on their target", () => {
    expect(g.nodes[0].data.globals).toEqual([{ index: 3, name: "Reset" }]);
    expect(g.edges.some((e) => e.data.index === 3)).toBe(false);
  });

  it("bends opposite edges apart", () => {
    const start = g.edges.find((e) => e.data.index === 0)!;
    const stop = g.edges.find((e) => e.data.index === 1)!;
    expect(start.data.offset).toBeGreaterThan(0);
    expect(stop.data.offset).toBeGreaterThan(0);
  });

  it("round-trips edge ids", () => {
    expect(parseEdgeId(edgeId(12, "In Review"))).toEqual({ index: 12, from: "In Review" });
    expect(parseEdgeId(edgeId(0, "a|b"))).toEqual({ index: 0, from: "a|b" });
    expect(parseEdgeId("nope")).toBeNull();
  });
});

describe("assignOffsets", () => {
  it("centres parallel edges in one direction", () => {
    const es = [0, 1, 2].map(() => ({ source: "a", target: "b", data: { offset: 99 } }));
    assignOffsets(es);
    expect(es.map((e) => e.data.offset)).toEqual([-PARALLEL_SPACING, 0, PARALLEL_SPACING]);
  });

  it("leaves a single edge straight", () => {
    const es = [{ source: "a", target: "b", data: { offset: 5 } }];
    assignOffsets(es);
    expect(es[0].data.offset).toBe(0);
  });
});

describe("geometry", () => {
  const box = { x: 0, y: 0, width: 100, height: 50 };

  it("finds where a ray leaves a rectangle", () => {
    expect(borderPoint(box, { x: 500, y: 25 })).toEqual({ x: 100, y: 25 });
    expect(borderPoint(box, { x: 50, y: -400 })).toEqual({ x: 50, y: 0 });
    expect(borderPoint(box, { x: 50, y: 25 })).toEqual({ x: 50, y: 25 });
  });

  it("draws straight and bent curves between boxes", () => {
    const right = { x: 300, y: 0, width: 100, height: 50 };
    const straight = edgePath(box, right, 0);
    expect(straight.path).toBe("M 101,25 Q 200,25 296,25");
    expect(straight.labelY).toBe(25);
    const bent = edgePath(box, right, 30);
    // Bends towards the perpendicular (downwards for a left-to-right edge) by roughly the offset.
    expect(bent.labelY).toBeGreaterThan(25 + 25);
    expect(bent.labelY).toBeLessThan(25 + 75);
    expect(edgePath(right, box, 30).labelY).toBeLessThan(0); // the reverse edge bends the other way
  });
});

describe("routeAroundNodes", () => {
  const box = (x: number) => ({ x, y: 100, width: 160, height: 60 });
  const rects = new Map([
    ["A", box(0)],
    ["B", box(260)],
    ["C", box(520)],
  ]);

  it("bends an edge that would cross another status", () => {
    const e = { source: "C", target: "A", data: { offset: 0 } };
    expect(curveHits(rects.get("C")!, rects.get("A")!, 0, [rects.get("B")!])).toBe(true);
    routeAroundNodes([e], rects);
    expect(e.data.offset).not.toBe(0);
    expect(curveHits(rects.get("C")!, rects.get("A")!, e.data.offset, [rects.get("B")!])).toBe(false);
  });

  it("leaves edges between neighbours alone", () => {
    const e = { source: "A", target: "B", data: { offset: 0 } };
    routeAroundNodes([e], rects);
    expect(e.data.offset).toBe(0);
  });

  it("keeps the side chosen for parallel edges", () => {
    const e = { source: "A", target: "C", data: { offset: -20 } };
    routeAroundNodes([e], rects);
    expect(e.data.offset).toBeLessThan(-20);
  });
});
