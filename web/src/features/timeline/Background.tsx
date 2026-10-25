import { memo, useMemo } from "react";
import { cn } from "@/lib/cn";
import { ROW_H } from "./Bar";
import type { Markers } from "./GridRow";
import { ReleaseLines, SprintBands } from "./Markers";
import { weekday } from "./model";
import { gridLines, type Zoom } from "./scale";

/**
 * Background is what sits behind the bars: weekend shading (week zoom), week or month lines,
 * each project's sprint bands and release lines across its rows, and the today line.
 */
export const Background = memo(function Background({
  range,
  zoom,
  x,
  ppd,
  width,
  height,
  spans,
  markers,
  today,
}: {
  range: { start: number; end: number };
  zoom: Zoom;
  x: (day: number) => number;
  ppd: number;
  width: number;
  height: number;
  spans: Map<string, [number, number]>;
  markers: Map<string, Markers>;
  today: number;
}) {
  const lines = useMemo(() => gridLines(range, zoom), [range, zoom]);
  const weekendOffset = ((6 - weekday(range.start) + 7) % 7) * ppd;
  return (
    <div aria-hidden className="pointer-events-none absolute inset-0" style={{ width, height }}>
      {zoom === "weeks" && (
        <div
          className="absolute inset-0"
          style={{
            backgroundImage: `repeating-linear-gradient(to right, color-mix(in srgb, var(--color-surface-3) 45%, transparent) 0 ${2 * ppd}px, transparent ${2 * ppd}px ${7 * ppd}px)`,
            backgroundPosition: `${weekendOffset}px 0`,
          }}
        />
      )}
      {lines.map((l) => (
        <div key={l.day} className={cn("absolute top-0 w-px", l.strong ? "bg-line-strong/70" : "bg-line/70")} style={{ left: x(l.day), height }} />
      ))}
      {[...spans].map(([key, [a, b]]) => {
        const m = markers.get(key);
        return m ? (
          <div key={key}>
            <SprintBands sprints={m.sprints} x={x} ppd={ppd} top={a * ROW_H} height={(b - a + 1) * ROW_H} />
            <ReleaseLines releases={m.releases} x={x} ppd={ppd} top={a * ROW_H} height={(b - a + 1) * ROW_H} />
          </div>
        ) : null;
      })}
      {today >= range.start && today <= range.end && <div className="absolute top-0 z-[1] w-0.5 bg-accent" style={{ left: x(today) + ppd / 2 - 1, height }} />}
    </div>
  );
});
