import { useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Schemas } from "@/api/client";
import { formatDay } from "@/features/backlog/dates";
import { C, CATEGORICAL, categoryColor, DataTable, Legend, Tooltip, TooltipRow } from "./ChartBits";
import { linear, niceTicks, useElementWidth } from "./chart-utils";
import { foldSeries, stackSeries } from "./series";

type CumulativeFlow = Schemas["CumulativeFlow"];

const M = { top: 12, bottom: 30, left: 40 };

/**
 * CumulativeFlowChart stacks the number of issues in each category (or status) at the end of
 * each day, done at the bottom. A band that widens is work piling up in that state. Colours
 * keep their meaning by category; by status, statuses take the categorical slots in workflow
 * order. Hover or arrow keys read a day.
 */
export function CumulativeFlowChart({ data, height: H = 260 }: { data: CumulativeFlow; height?: number }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const byCategory = data.series.every((s) => s.key === s.category);
  const series = foldSeries(data.series, CATEGORICAL.length).map((s, k) => ({
    ...s,
    color: s.key === "" ? C.reference : byCategory ? categoryColor[s.category] : CATEGORICAL[k],
  }));
  const n = data.dates.length;
  // Done sits at the bottom: stack in reverse workflow order.
  const bands = stackSeries([...series].reverse().map((s) => s.counts));
  const totals = data.dates.map((_, i) => series.reduce((t, s) => t + (s.counts[i] ?? 0), 0));
  const labelled = width >= 440 && series.length <= 4;
  const right = labelled ? 96 : 16;
  const x = linear([0, Math.max(1, n - 1)], [M.left, width - right]);
  const ticks = niceTicks(Math.max(1, ...totals), H < 240 ? 4 : 5);
  const y = linear([0, ticks[ticks.length - 1]], [H - M.bottom, M.top]);
  const last = n - 1;
  if (n === 0) return null;

  const area = (k: number) => {
    const { lower, upper } = bands[k];
    const top = upper.map((v, i) => `${i === 0 ? "M" : "L"}${x(i)},${y(v)}`).join("");
    const bottom = lower.map((v, i) => `L${x(i)},${y(v)}`).reverse().join("");
    return `${top}${bottom}Z`;
  };
  const edge = (k: number) => bands[k].upper.map((v, i) => `${i === 0 ? "M" : "L"}${x(i)},${y(v)}`).join("");
  const tickEvery = Math.max(1, Math.ceil(n / Math.max(2, Math.floor((width - M.left - right) / 80))));
  // Every few days, plus the last day (dropping a tick that would crowd it).
  const xTicks = data.dates.map((_, i) => i).filter((i) => i % tickEvery === 0 && last - i >= tickEvery).concat(last > 0 ? [last] : []);

  const move = (e: PointerEvent<SVGRectElement>) => {
    const box = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
    const i = Math.round(((e.clientX - box.left - M.left) / Math.max(1, width - M.left - right)) * Math.max(1, last));
    setHover(Math.max(0, Math.min(last, i)));
  };
  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    setHover((h) => Math.max(0, Math.min(last, (h ?? last) + (e.key === "ArrowLeft" ? -1 : 1))));
  };
  const stackIndex = (k: number) => series.length - 1 - k;
  const summary = `Cumulative flow over ${n} days. On ${formatDay(data.dates[last])}: ${series.map((s) => `${s.counts[last] ?? 0} ${s.label}`).join(", ")}`;

  return (
    <div>
      <div className="mb-2">
        <Legend items={series.map((s) => ({ label: s.label, color: s.color, shape: "rect" as const }))} />
      </div>
      <div ref={ref} className="relative">
        <svg
          width={width}
          height={H}
          role="img"
          aria-label={summary}
          tabIndex={0}
          onKeyDown={onKey}
          onFocus={() => setHover(last)}
          onBlur={() => setHover(null)}
          className="block overflow-visible rounded focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
        >
          {ticks.map((t) => (
            <g key={t}>
              <line x1={M.left} x2={width - right} y1={y(t)} y2={y(t)} stroke={t === 0 ? C.axis : C.grid} strokeWidth={1} />
              <text x={M.left - 8} y={y(t)} dy="0.32em" textAnchor="end" fontSize={11} fill={C.text} className="tabular-nums">
                {t}
              </text>
            </g>
          ))}
          {xTicks.map((i) => (
            <text key={i} x={x(i)} y={H - M.bottom + 18} textAnchor={i === 0 ? "start" : i === last ? "end" : "middle"} fontSize={11} fill={C.text}>
              {formatDay(data.dates[i])}
            </text>
          ))}
          {bands.map((_, k) => {
            const s = series[stackIndex(k)];
            return (
              <g key={s.key || "other"}>
                <path d={area(k)} fill={s.color} fillOpacity={0.24} />
                <path d={edge(k)} fill="none" stroke={s.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
              </g>
            );
          })}
          {labelled &&
            bands.map((b, k) => {
              const s = series[stackIndex(k)];
              const top = y(b.upper[last]);
              const bottom = y(b.lower[last]);
              if (bottom - top < 14) return null;
              return (
                <text key={s.key} x={width - right + 8} y={(top + bottom) / 2} dy="0.32em" fontSize={11} fill={C.text}>
                  <tspan fontWeight={600} fill="var(--color-ink)">
                    {s.counts[last] ?? 0}
                  </tspan>{" "}
                  {s.label}
                </text>
              );
            })}
          {hover != null && (
            <g pointerEvents="none">
              <line x1={x(hover)} x2={x(hover)} y1={M.top} y2={H - M.bottom} stroke={C.text} strokeWidth={1} />
              {bands.map((b, k) => (
                <circle key={k} cx={x(hover)} cy={y(b.upper[hover])} r={4} fill={series[stackIndex(k)].color} stroke={C.surface} strokeWidth={2} />
              ))}
            </g>
          )}
          <rect x={M.left} y={M.top} width={Math.max(0, width - M.left - right)} height={H - M.top - M.bottom} fill="transparent" onPointerMove={move} onPointerLeave={() => setHover(null)} />
        </svg>
        {hover != null && (
          <Tooltip x={x(hover)} y={y(totals[hover])} width={width}>
            <p className="mb-1 font-medium text-ink">{formatDay(data.dates[hover])}</p>
            {series.map((s) => (
              <TooltipRow key={s.key || "other"} color={s.color} value={s.counts[hover] ?? 0} label={s.label} />
            ))}
            <TooltipRow value={totals[hover]} label="in total" />
          </Tooltip>
        )}
      </div>
      <DataTable head={["Date", ...series.map((s) => s.label)]} rows={data.dates.map((d, i) => [formatDay(d), ...series.map((s) => s.counts[i] ?? 0)])} />
    </div>
  );
}
