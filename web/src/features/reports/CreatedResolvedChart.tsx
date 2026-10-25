import { useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Schemas } from "@/api/client";
import { addDays, formatDay } from "@/features/backlog/dates";
import { barPath, C, DataTable, Legend, Tooltip, TooltipRow } from "./ChartBits";
import { linear, niceTicks, useElementWidth } from "./chart-utils";

type CreatedResolved = Schemas["CreatedResolved"];

const M = { top: 12, right: 16, bottom: 30, left: 40 };
const STRIP = 48;
const STRIP_GAP = 34;
const GAP = 2;

/**
 * CreatedResolvedChart compares issues created and resolved per day or week: paired columns
 * when there is room, lines when the buckets are dense. Below, on its own scale (never a
 * second axis on the same plot), the number open at the end of each bucket.
 */
export function CreatedResolvedChart({ data, height: H = 280 }: { data: CreatedResolved; height?: number }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const b = data.buckets;
  const n = b.length;
  const plotW = Math.max(0, width - M.left - M.right);
  const slot = plotW / Math.max(1, n);
  const bars = slot >= 16;
  const bar = Math.max(3, Math.min(12, (slot - 6) / 2));
  const center = (i: number) => M.left + slot * (i + 0.5);
  const mainBottom = H - M.bottom - STRIP - STRIP_GAP;
  const stripTop = H - M.bottom - STRIP;
  const stripBottom = H - M.bottom;
  const ticks = niceTicks(Math.max(1, ...b.flatMap((k) => [k.created, k.resolved])), 4, true);
  const y = linear([0, ticks[ticks.length - 1]], [mainBottom, M.top]);
  const openTicks = niceTicks(Math.max(1, ...b.map((k) => k.open)), 2, true);
  const yOpen = linear([0, openTicks[openTicks.length - 1]], [stripBottom, stripTop]);
  const last = n - 1;
  if (n === 0) return null;

  const per = data.interval === "week" ? "week" : "day";
  const label = (i: number) => (per === "week" ? `Week of ${formatDay(b[i].start)}` : formatDay(b[i].start));
  const line = (v: (k: (typeof b)[number]) => number, sy: (n: number) => number) => b.map((k, i) => `${i === 0 ? "M" : "L"}${center(i)},${sy(v(k))}`).join("");
  const series = [
    { key: "created", label: "Created", color: C.series, value: (k: (typeof b)[number]) => k.created },
    { key: "resolved", label: "Resolved", color: C.done, value: (k: (typeof b)[number]) => k.resolved },
  ];
  const tickEvery = Math.max(1, Math.ceil(n / Math.max(2, Math.floor(plotW / 80))));
  const xTicks = b.map((_, i) => i).filter((i) => i % tickEvery === 0);
  const created = b.reduce((t, k) => t + k.created, 0);
  const resolved = b.reduce((t, k) => t + k.resolved, 0);

  const move = (e: PointerEvent<SVGRectElement>) => {
    const box = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
    setHover(Math.max(0, Math.min(last, Math.floor((e.clientX - box.left - M.left) / slot))));
  };
  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    setHover((h) => Math.max(0, Math.min(last, (h ?? last) + (e.key === "ArrowLeft" ? -1 : 1))));
  };
  const h = hover != null ? b[hover] : null;

  return (
    <div>
      <div className="mb-2">
        <Legend
          items={[
            ...series.map((s) => ({ label: s.label, color: s.color, shape: bars ? ("rect" as const) : ("line" as const) })),
            { label: "Open", color: C.text, shape: "line" as const },
          ]}
        />
      </div>
      <div ref={ref} className="relative">
        <svg
          width={width}
          height={H}
          role="img"
          aria-label={`Created vs resolved, ${n} ${per}s: ${created} created, ${resolved} resolved, ${b[last].open} open at the end`}
          tabIndex={0}
          onKeyDown={onKey}
          onFocus={() => setHover(last)}
          onBlur={() => setHover(null)}
          className="block overflow-visible rounded focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
        >
          {ticks.map((t) => (
            <g key={t}>
              <line x1={M.left} x2={width - M.right} y1={y(t)} y2={y(t)} stroke={t === 0 ? C.axis : C.grid} strokeWidth={1} />
              <text x={M.left - 8} y={y(t)} dy="0.32em" textAnchor="end" fontSize={11} fill={C.text} className="tabular-nums">
                {t}
              </text>
            </g>
          ))}
          {bars
            ? b.map((k, i) => (
                <g key={k.start} opacity={hover == null || hover === i ? 1 : 0.55}>
                  <path d={barPath(center(i) - GAP / 2 - bar, y(k.created), bar, y(0) - y(k.created), 3)} fill={C.series} />
                  <path d={barPath(center(i) + GAP / 2, y(k.resolved), bar, y(0) - y(k.resolved), 3)} fill={C.done} />
                </g>
              ))
            : series.map((s) => <path key={s.key} d={line(s.value, y)} fill="none" stroke={s.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />)}

          {/* The open trend: its own small plot, sharing the x positions. */}
          <text x={M.left} y={stripTop - 8} fontSize={11} fill={C.text}>
            Open at the end of each {per}
          </text>
          {openTicks.map((t, k) =>
            k === 0 || k === openTicks.length - 1 ? (
              <g key={t}>
                <line x1={M.left} x2={width - M.right} y1={yOpen(t)} y2={yOpen(t)} stroke={t === 0 ? C.axis : C.grid} strokeWidth={1} />
                <text x={M.left - 8} y={yOpen(t)} dy="0.32em" textAnchor="end" fontSize={11} fill={C.text} className="tabular-nums">
                  {t}
                </text>
              </g>
            ) : null,
          )}
          <path d={`${line((k) => k.open, yOpen)}L${center(last)},${stripBottom}L${center(0)},${stripBottom}Z`} fill={C.text} fillOpacity={0.1} />
          <path d={line((k) => k.open, yOpen)} fill="none" stroke={C.text} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          <circle cx={center(last)} cy={yOpen(b[last].open)} r={4} fill={C.text} stroke={C.surface} strokeWidth={2} />

          {xTicks.map((i) => (
            <text key={i} x={center(i)} y={H - M.bottom + 18} textAnchor={i === 0 && !bars ? "start" : "middle"} fontSize={11} fill={C.text}>
              {formatDay(b[i].start)}
            </text>
          ))}
          {hover != null && h && (
            <g pointerEvents="none">
              <line x1={center(hover)} x2={center(hover)} y1={M.top} y2={stripBottom} stroke={C.text} strokeWidth={1} />
              {!bars && series.map((s) => <circle key={s.key} cx={center(hover)} cy={y(s.value(h))} r={4} fill={s.color} stroke={C.surface} strokeWidth={2} />)}
              <circle cx={center(hover)} cy={yOpen(h.open)} r={4} fill={C.text} stroke={C.surface} strokeWidth={2} />
            </g>
          )}
          <rect x={M.left} y={M.top} width={plotW} height={stripBottom - M.top} fill="transparent" onPointerMove={move} onPointerLeave={() => setHover(null)} />
        </svg>
        {hover != null && h && (
          <Tooltip x={center(hover) + (bars ? bar : 0)} y={y(Math.max(h.created, h.resolved))} width={width}>
            <p className="mb-1 font-medium text-ink">{label(hover)}</p>
            {series.map((s) => (
              <TooltipRow key={s.key} color={s.color} value={s.value(h)} label={s.label.toLowerCase()} />
            ))}
            <TooltipRow color={C.text} value={h.open} label={`open at the end of the ${per}`} />
          </Tooltip>
        )}
      </div>
      <DataTable
        head={[per === "week" ? "Week of" : "Date", "Created", "Resolved", "Open"]}
        rows={b.map((k) => [per === "week" ? `${formatDay(k.start)} – ${formatDay(addDays(k.start, 6))}` : formatDay(k.start), k.created, k.resolved, k.open])}
      />
    </div>
  );
}
