import { useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Schemas } from "@/api/client";
import { addDays, daysBetween, formatDay } from "@/features/backlog/dates";
import { formatPoints } from "@/features/board/model";
import { C, DataTable, Legend, Tooltip, TooltipRow } from "@/features/reports/ChartBits";
import { linear, niceTicks, useElementWidth } from "@/features/reports/chart-utils";
import type { ReleaseBurnup } from "./queries";

type Day = Schemas["ReleaseBurnupDay"];
export type BurnupMeasure = "issues" | "points";

const M = { top: 20, right: 20, bottom: 30, left: 40 };

/** burnupCaption says where the scope line comes from. */
export function burnupCaption(b: Pick<ReleaseBurnup, "scopeSource">): string {
  return b.scopeSource === "history"
    ? "Scope follows the fix version changes in each issue's history: issues added to the release raise it, issues taken out lower it."
    : "No issue's fix versions have changed since it was created, so scope is the release's current issues, each counted from its creation.";
}

/**
 * ReleaseBurnupChart plots the release's done work against its scope at the end of each day,
 * from the start date to today (or the day it was released), with the release date marked.
 * Hover or arrow keys read a day.
 */
export function ReleaseBurnupChart({ burnup, measure, height: H = 260 }: { burnup: ReleaseBurnup; measure: BurnupMeasure; height?: number }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const { days: data, from } = burnup;
  if (data.length === 0) return null;
  const pts = measure === "points";
  const done = (d: Day) => (pts ? d.donePoints : d.doneIssues);
  const scope = (d: Day) => (pts ? d.scopePoints : d.scopeIssues);
  const fmt = (n: number) => (pts ? formatPoints(n) : String(Math.round(n)));
  const unit = pts ? "pts" : "issues";
  const lastDay = data[data.length - 1].date;
  const releaseAt = burnup.releaseDate && burnup.releaseDate >= from ? daysBetween(from, burnup.releaseDate) : null;
  const days = Math.max(1, daysBetween(from, lastDay), releaseAt ?? 0);
  const x = linear([0, days], [M.left, width - M.right]);
  const ticks = niceTicks(Math.max(1, ...data.map(scope)), H < 240 ? 4 : 5, !pts);
  const y = linear([0, ticks[ticks.length - 1]], [H - M.bottom, M.top]);
  const series = data.map((d) => ({ d, i: daysBetween(from, d.date) }));
  const last = series[series.length - 1];
  const lines = [
    { key: "done", label: "Done", color: C.series, value: done },
    { key: "scope", label: "Scope", color: C.second, value: scope },
  ];
  const path = (value: (d: Day) => number) => series.map(({ d, i }, k) => (k === 0 ? `M${x(i)},${y(value(d))}` : `H${x(i)}V${y(value(d))}`)).join("");
  const step = Math.max(1, Math.ceil(64 / ((width - M.left - M.right) / days)));
  const xTicks = Array.from({ length: Math.floor(days / step) + 1 }, (_, k) => k * step);
  const at = (i: number) => series.filter((s) => s.i <= i).pop()?.d;
  const move = (e: PointerEvent<SVGRectElement>) => {
    const box = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
    setHover(Math.max(0, Math.min(days, Math.round(((e.clientX - box.left - M.left) / (width - M.left - M.right)) * days))));
  };
  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    setHover((h) => Math.max(0, Math.min(days, (h ?? last.i) + (e.key === "ArrowLeft" ? -1 : 1))));
  };
  const hovered = hover != null && hover <= last.i ? at(hover) : undefined;
  const summary = `Burnup: ${fmt(done(last.d))} of ${fmt(scope(last.d))} ${unit} done on ${formatDay(last.d.date)}`;
  const doneLabelClear = Math.abs(y(done(last.d)) - y(scope(last.d))) >= 14;
  // End labels sit left of the last point, or right of it while the chart has days ahead.
  const labelLeft = last.i > days / 2;

  return (
    <div>
      <div className="mb-2">
        <Legend
          items={[
            ...lines.map((l) => ({ label: l.label, color: l.color, shape: "line" as const })),
            ...(releaseAt != null ? [{ label: "Release date", color: C.reference, shape: "line" as const }] : []),
          ]}
        />
      </div>
      <div ref={ref} className="relative">
        <svg
          width={width}
          height={H}
          role="img"
          aria-label={summary}
          tabIndex={0}
          onKeyDown={onKey}
          onFocus={() => setHover(last.i)}
          onBlur={() => setHover(null)}
          className="block overflow-visible rounded focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
        >
          {ticks.map((t) => (
            <g key={t}>
              <line x1={M.left} x2={width - M.right} y1={y(t)} y2={y(t)} stroke={t === 0 ? C.axis : C.grid} strokeWidth={1} />
              <text x={M.left - 8} y={y(t)} dy="0.32em" textAnchor="end" fontSize={11} fill={C.text} className="tabular-nums">
                {fmt(t)}
              </text>
            </g>
          ))}
          {xTicks.map((i) => (
            <text key={i} x={x(i)} y={H - M.bottom + 18} textAnchor={i === 0 ? "start" : i === days ? "end" : "middle"} fontSize={11} fill={C.text}>
              {formatDay(addDays(from, i))}
            </text>
          ))}
          {releaseAt != null && (
            <g>
              <line x1={x(releaseAt)} x2={x(releaseAt)} y1={M.top - 6} y2={H - M.bottom} stroke={C.reference} strokeWidth={2} strokeDasharray="4 3" />
              <text x={x(releaseAt)} y={M.top - 9} textAnchor={releaseAt > days * 0.8 ? "end" : "middle"} fontSize={10} fontWeight={600} fill={C.text}>
                Release date
              </text>
            </g>
          )}
          {[...lines].reverse().map((l) => (
            <path key={l.key} d={path(l.value)} fill="none" stroke={l.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          ))}
          {lines.map((l, k) => (
            <g key={l.key}>
              <circle cx={x(last.i)} cy={y(l.value(last.d))} r={4} fill={l.color} stroke={C.surface} strokeWidth={2} />
              {(k === 1 || doneLabelClear) && (
                <text x={x(last.i) + (labelLeft ? -6 : 8)} y={y(l.value(last.d)) - 9} textAnchor={labelLeft ? "end" : "start"} fontSize={11} fontWeight={600} fill="var(--color-ink)">
                  {fmt(l.value(last.d))} {k === 0 ? "done" : "scope"}
                </text>
              )}
            </g>
          ))}
          {hover != null && (
            <g pointerEvents="none">
              <line x1={x(hover)} x2={x(hover)} y1={M.top} y2={H - M.bottom} stroke={C.text} strokeWidth={1} />
              {hovered && lines.map((l) => <circle key={l.key} cx={x(hover)} cy={y(l.value(hovered))} r={4} fill={l.color} stroke={C.surface} strokeWidth={2} />)}
            </g>
          )}
          <rect x={M.left} y={M.top} width={Math.max(0, width - M.left - M.right)} height={H - M.top - M.bottom} fill="transparent" onPointerMove={move} onPointerLeave={() => setHover(null)} />
        </svg>
        {hover != null && (
          <Tooltip x={x(hover)} y={hovered ? y(done(hovered)) : M.top} width={width}>
            <p className="mb-1 font-medium text-ink">{formatDay(addDays(from, hover))}</p>
            {hovered ? (
              lines.map((l) => <TooltipRow key={l.key} color={l.color} value={`${fmt(l.value(hovered))} ${unit}`} label={l.label.toLowerCase()} />)
            ) : (
              <p className="text-muted">Not yet</p>
            )}
          </Tooltip>
        )}
      </div>
      <DataTable head={["Date", "Done", "Scope"]} rows={data.map((d) => [formatDay(d.date), fmt(done(d)), fmt(scope(d))])} />
    </div>
  );
}
