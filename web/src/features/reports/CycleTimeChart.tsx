import { useState } from "react";
import type { Schemas } from "@/api/client";
import { formatDay, toISODate } from "@/features/backlog/dates";
import { C, DataTable, Tooltip, TooltipRow } from "./ChartBits";
import { durationUnit, formatDuration, linear, niceTicks, useElementWidth } from "./chart-utils";

type CycleTimeReport = Schemas["CycleTimeReport"];

const H = 280;
const M = { top: 16, right: 76, bottom: 30, left: 44 };
const DAY = 86_400_000;

/**
 * CycleTimeChart scatters resolved issues by resolution date and cycle time, with the median
 * and 85th percentile as reference lines. Dots are links to the issues.
 */
export function CycleTimeChart({ report, onOpen }: { report: CycleTimeReport; onOpen: (key: string) => void }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const now = Date.now();
  const from = now - report.days * DAY;
  const x = linear([from, now], [M.left, width - M.right]);
  const maxHours = Math.max(report.p85Hours, ...report.issues.map((i) => i.cycleHours), 1);
  const unit = durationUnit(maxHours);
  const per = unit === "d" ? 24 : 1;
  const ticks = niceTicks(maxHours / per, 5);
  const y = linear([0, ticks[ticks.length - 1] * per], [H - M.bottom, M.top]);
  const xTickCount = Math.max(2, Math.min(7, Math.floor((width - M.left - M.right) / 90)));
  const xTicks = Array.from({ length: xTickCount }, (_, k) => from + ((now - from) * k) / (xTickCount - 1));
  const refs = [
    { label: "85%", hours: report.p85Hours },
    { label: "Median", hours: report.medianHours },
  ];
  const h = hover != null ? report.issues[hover] : null;

  return (
    <div>
      <div ref={ref} className="relative">
        <svg width={width} height={H} role="img" aria-label={`Cycle time: median ${formatDuration(report.medianHours)}, 85th percentile ${formatDuration(report.p85Hours)}`} className="block overflow-visible">
          {ticks.map((t) => (
            <g key={t}>
              <line x1={M.left} x2={width - M.right} y1={y(t * per)} y2={y(t * per)} stroke={t === 0 ? C.axis : C.grid} strokeWidth={1} />
              <text x={M.left - 8} y={y(t * per)} dy="0.32em" textAnchor="end" fontSize={11} fill={C.text} className="tabular-nums">
                {t}
                {unit}
              </text>
            </g>
          ))}
          {xTicks.map((t, k) => (
            <text key={t} x={x(t)} y={H - M.bottom + 18} textAnchor={k === 0 ? "start" : k === xTicks.length - 1 ? "end" : "middle"} fontSize={11} fill={C.text}>
              {formatDay(toISODate(new Date(t)))}
            </text>
          ))}
          {report.issues.length > 0 &&
            refs.map((r) => (
              <g key={r.label} pointerEvents="none">
                <line x1={M.left} x2={width - M.right} y1={y(r.hours)} y2={y(r.hours)} stroke={C.text} strokeWidth={1} />
                <text x={width - M.right + 6} y={y(r.hours)} dy="0.32em" fontSize={11} fill={C.text}>
                  <tspan fontWeight={600} fill="var(--color-ink)">
                    {formatDuration(r.hours, unit)}
                  </tspan>{" "}
                  {r.label}
                </text>
              </g>
            ))}
          {report.issues.map((i, k) => {
            const cx = x(new Date(i.resolvedAt).getTime());
            const cy = y(i.cycleHours);
            return (
              <a
                key={i.key}
                href={`/browse/${i.key}`}
                aria-label={`${i.key}: ${formatDuration(i.cycleHours)}`}
                onClick={(e) => {
                  e.preventDefault();
                  onOpen(i.key);
                }}
                onPointerEnter={() => setHover(k)}
                onPointerLeave={() => setHover(null)}
                onFocus={() => setHover(k)}
                onBlur={() => setHover(null)}
                className="outline-none [&:focus-visible>circle:first-child]:stroke-accent"
              >
                <circle cx={cx} cy={cy} r={12} fill="transparent" strokeWidth={2} />
                <circle cx={cx} cy={cy} r={hover === k ? 5.5 : 4} fill={C.series} stroke={C.surface} strokeWidth={2} pointerEvents="none" />
              </a>
            );
          })}
        </svg>
        {h && hover != null && (
          <Tooltip x={x(new Date(h.resolvedAt).getTime())} y={y(h.cycleHours)} width={width}>
            <p className="font-medium text-ink">
              <span className="font-mono text-muted">{h.key}</span> {h.summary.length > 48 ? `${h.summary.slice(0, 47)}…` : h.summary}
            </p>
            <TooltipRow color={C.series} value={formatDuration(h.cycleHours)} label={`resolved ${formatDay(toISODate(new Date(h.resolvedAt)))}`} />
          </Tooltip>
        )}
      </div>
      <DataTable
        head={["Issue", "Summary", "Cycle time", "Resolved"]}
        rows={[...report.issues]
          .sort((a, b) => b.cycleHours - a.cycleHours)
          .map((i) => [i.key, i.summary, formatDuration(i.cycleHours), formatDay(toISODate(new Date(i.resolvedAt)))])}
      />
    </div>
  );
}
