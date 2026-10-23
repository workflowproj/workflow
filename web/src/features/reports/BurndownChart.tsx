import { useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Schemas } from "@/api/client";
import { addDays, daysBetween, formatDay, todayISO } from "@/features/backlog/dates";
import { formatPoints } from "@/features/board/model";
import { C, DataTable, Legend, Tooltip, TooltipRow } from "./ChartBits";
import { linear, niceTicks, useElementWidth } from "./chart-utils";

type SprintReport = Schemas["SprintReport"];

const H = 280;
const M = { top: 20, right: 20, bottom: 30, left: 40 };

/**
 * BurndownChart plots story points remaining per day of the sprint (a step line) against the
 * guideline from the commitment down to zero on the end date. Hover or arrow keys read a day.
 */
export function BurndownChart({ report }: { report: SprintReport }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const { sprint, burndown, committedPoints } = report;
  const start = sprint.startDate ?? burndown[0]?.date;
  if (!start || burndown.length === 0) return null;
  const end = sprint.endDate && sprint.endDate > start ? sprint.endDate : burndown[burndown.length - 1].date;
  const days = Math.max(1, daysBetween(start, end));
  const x = linear([0, days], [M.left, width - M.right]);
  const ticks = niceTicks(Math.max(committedPoints, ...burndown.map((p) => p.remainingPoints), 1), 5);
  const y = linear([0, ticks[ticks.length - 1]], [H - M.bottom, M.top]);
  const pts = burndown.map((p) => ({ ...p, i: daysBetween(start, p.date) }));
  const ideal = (i: number) => Math.max(0, committedPoints * (1 - i / days));

  // Remaining holds until the next day's value: a step-after line.
  const path = pts.map((p, k) => (k === 0 ? `M${x(p.i)},${y(p.remainingPoints)}` : `H${x(p.i)}V${y(p.remainingPoints)}`)).join("");
  const last = pts[pts.length - 1];
  const today = daysBetween(start, todayISO());
  const showToday = sprint.state === "active" && today >= 0 && today <= days;
  const step = Math.max(1, Math.ceil(64 / ((width - M.left - M.right) / days)));
  const xTicks = Array.from({ length: Math.floor(days / step) + 1 }, (_, k) => k * step);

  const at = (i: number) => pts.filter((p) => p.i <= i).pop();
  const move = (e: PointerEvent<SVGRectElement>) => {
    const box = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
    const i = Math.round(((e.clientX - box.left - M.left) / (width - M.left - M.right)) * days);
    setHover(Math.max(0, Math.min(days, i)));
  };
  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    setHover((h) => Math.max(0, Math.min(days, (h ?? last.i) + (e.key === "ArrowLeft" ? -1 : 1))));
  };
  const hovered = hover != null ? { i: hover, remaining: hover <= last.i ? at(hover)?.remainingPoints : undefined } : null;

  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-3">
        <Legend
          items={[
            { label: "Remaining", color: C.series, shape: "line" },
            { label: "Guideline", color: C.reference, shape: "line" },
          ]}
        />
      </div>
      <div ref={ref} className="relative">
        <svg
          width={width}
          height={H}
          role="img"
          aria-label={`Burndown: ${formatPoints(last.remainingPoints)} of ${formatPoints(committedPoints)} points remaining`}
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
                {formatPoints(t)}
              </text>
            </g>
          ))}
          {xTicks.map((i) => (
            <text key={i} x={x(i)} y={H - M.bottom + 18} textAnchor={i === 0 ? "start" : i === days ? "end" : "middle"} fontSize={11} fill={C.text}>
              {formatDay(addDays(start, i))}
            </text>
          ))}
          {showToday && (
            <g>
              <line x1={x(today)} x2={x(today)} y1={M.top - 6} y2={H - M.bottom} stroke={C.axis} strokeWidth={1} />
              <text x={x(today)} y={M.top - 9} textAnchor="middle" fontSize={10} fontWeight={600} fill={C.text}>
                Today
              </text>
            </g>
          )}
          <line x1={x(0)} y1={y(committedPoints)} x2={x(days)} y2={y(0)} stroke={C.reference} strokeWidth={2} strokeLinecap="round" />
          <path d={path} fill="none" stroke={C.series} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          <circle cx={x(last.i)} cy={y(last.remainingPoints)} r={4} fill={C.series} stroke={C.surface} strokeWidth={2} />
          <text x={x(last.i) + 8} y={y(last.remainingPoints) - 8} fontSize={11} fontWeight={600} fill="var(--color-ink)">
            {formatPoints(last.remainingPoints)} left
          </text>
          {hovered && (
            <g pointerEvents="none">
              <line x1={x(hovered.i)} x2={x(hovered.i)} y1={M.top} y2={H - M.bottom} stroke={C.text} strokeWidth={1} />
              {hovered.remaining != null && <circle cx={x(hovered.i)} cy={y(hovered.remaining)} r={4} fill={C.series} stroke={C.surface} strokeWidth={2} />}
            </g>
          )}
          <rect x={M.left} y={M.top} width={Math.max(0, width - M.left - M.right)} height={H - M.top - M.bottom} fill="transparent" onPointerMove={move} onPointerLeave={() => setHover(null)} />
        </svg>
        {hovered && (
          <Tooltip x={x(hovered.i)} y={y(hovered.remaining ?? ideal(hovered.i))} width={width}>
            <p className="mb-1 font-medium text-ink">{formatDay(addDays(start, hovered.i))}</p>
            {hovered.remaining != null && <TooltipRow color={C.series} value={`${formatPoints(hovered.remaining)} pts`} label="remaining" />}
            <TooltipRow color={C.reference} value={`${formatPoints(Math.round(ideal(hovered.i) * 10) / 10)} pts`} label="guideline" />
          </Tooltip>
        )}
      </div>
      <DataTable head={["Date", "Remaining", "Guideline"]} rows={pts.map((p) => [formatDay(p.date), formatPoints(p.remainingPoints), formatPoints(Math.round(ideal(p.i) * 10) / 10)])} />
    </div>
  );
}
