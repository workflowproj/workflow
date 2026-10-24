import type { KeyboardEvent, MouseEvent } from "react";
import { ROW_H } from "./Bar";
import type { Dependency, TimelineLink } from "./model";

/** route draws a dependency from the blocker's end to the blocked issue's start, elbowing between rows. */
export function route(x1: number, r1: number, x2: number, r2: number): string {
  const y1 = r1 * ROW_H + ROW_H / 2;
  const y2 = r2 * ROW_H + ROW_H / 2;
  if (x2 - x1 >= 16) return `M${x1},${y1}H${x1 + 8}V${y2}H${x2}`;
  // Backwards: leave to the right, run along the gap between rows, come in from the left.
  const gap = r2 > r1 ? (r1 + 1) * ROW_H : r1 * ROW_H;
  return `M${x1},${y1}H${x1 + 8}V${gap}H${x2 - 10}V${y2}H${x2}`;
}

/**
 * Dependencies is the SVG layer of "blocks" arrows. Conflicting ones (the blocker ends after
 * the blocked issue starts) are red. Arrows are clickable (and focusable) to inspect or remove.
 */
export function Dependencies({
  deps,
  rowOf,
  x,
  width,
  height,
  selected,
  onSelect,
}: {
  deps: Dependency[];
  rowOf: Map<string, number>;
  x: (day: number) => number;
  width: number;
  height: number;
  selected?: string;
  onSelect: (link: TimelineLink, at: { clientX: number; clientY: number }) => void;
}) {
  const drawn = deps.flatMap((d) => {
    const r1 = rowOf.get(d.link.sourceKey);
    const r2 = rowOf.get(d.link.targetKey);
    if (r1 == null || r2 == null || !d.from || !d.to) return [];
    return [{ d, path: route(x(d.from.end + 1), r1, x(d.to.start), r2) }];
  });
  const pick = (link: TimelineLink) => (e: MouseEvent | KeyboardEvent) => {
    if ("key" in e && e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    e.stopPropagation();
    const box = (e.currentTarget as SVGElement).getBoundingClientRect();
    onSelect(link, "clientX" in e ? { clientX: e.clientX, clientY: e.clientY } : { clientX: box.left + box.width / 2, clientY: box.top + box.height / 2 });
  };
  return (
    <svg className="pointer-events-none absolute top-0 left-0 z-[1] overflow-visible" width={width} height={height} aria-label="Dependencies">
      <defs>
        <marker id="tl-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M0,0.5 L7.5,4 L0,7.5 z" fill="var(--color-muted)" />
        </marker>
        <marker id="tl-arrow-conflict" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M0,0.5 L7.5,4 L0,7.5 z" fill="var(--color-danger)" />
        </marker>
      </defs>
      {drawn.map(({ d, path }) => {
        const active = selected === d.link.id;
        const pending = d.link.id.startsWith("pending-");
        return (
          <g key={d.link.id} data-link-id={d.link.id} data-conflict={d.conflict || undefined}>
            <path
              d={path}
              fill="none"
              stroke={d.conflict ? "var(--color-danger)" : "var(--color-muted)"}
              strokeOpacity={d.conflict || active ? 1 : 0.7}
              strokeWidth={active ? 2.5 : 1.5}
              strokeDasharray={pending ? "4 3" : undefined}
              strokeLinejoin="round"
              markerEnd={`url(#${d.conflict ? "tl-arrow-conflict" : "tl-arrow"})`}
            />
            <path
              d={path}
              fill="none"
              stroke="transparent"
              strokeWidth={10}
              className="cursor-pointer outline-none"
              style={{ pointerEvents: "stroke" }}
              tabIndex={0}
              role="button"
              aria-label={`${d.link.sourceKey} blocks ${d.link.targetKey}${d.conflict ? " (conflict)" : ""}`}
              onClick={pick(d.link)}
              onKeyDown={pick(d.link)}
            />
          </g>
        );
      })}
    </svg>
  );
}
