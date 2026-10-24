import { useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Schemas } from "@/api/client";
import { addDays, daysBetween, formatDay, todayISO } from "@/features/backlog/dates";
import { formatPoints } from "@/features/board/model";
import { C, DataTable, Legend, Tooltip, TooltipRow } from "./ChartBits";
import { linear, niceTicks, useElementWidth } from "./chart-utils";

type SprintReport = Schemas["SprintReport"];
type BurndownPoint = Schemas["BurndownPoint"];

/** Sprint charts count story points, or issues when a sprint isn't estimated. */
export type Measure = "points" | "issues";

const M = { top: 20, right: 20, bottom: 30, left: 40 };

/** sprintMeasure picks points when the sprint has any, else issue counts. */
export function sprintMeasure(report: SprintReport, preferred?: Measure): Measure {
  if (preferred) return preferred;
  const pointed = report.committedPoints > 0 || report.addedPoints > 0 || report.burndown.some((p) => p.scopePoints > 0);
  return pointed ? "points" : "issues";
}

/** measureOf reads a sprint report's totals and daily values in one measure. */
export function measureOf(report: SprintReport, measure: Measure) {
  const pts = measure === "points";
  return {
    committed: pts ? report.committedPoints : report.committedIssues,
    completed: pts ? report.completedPoints : report.completedIssues,
    added: pts ? report.addedPoints : report.addedIssues,
    removed: pts ? report.removedPoints : report.removedIssues,
    remaining: (p: BurndownPoint) => (pts ? p.remainingPoints : p.remainingIssues),
    scope: (p: BurndownPoint) => (pts ? p.scopePoints : p.scopeIssues),
    done: (p: BurndownPoint) => (pts ? p.completedPoints : p.completedIssues),
    unit: pts ? "pts" : "issues",
    format: (n: number) => (pts ? formatPoints(n) : String(Math.round(n))),
  };
}

/**
 * BurndownChart plots the work remaining at the end of each day of the sprint (a step line;
 * it rises when work is added) against the guideline from the commitment down to zero on the
 * end date. Hover or arrow keys read a day.
 */
export function BurndownChart({ report, measure, height }: { report: SprintReport; measure?: Measure; height?: number }) {
  return <SprintDayChart report={report} kind="burndown" measure={sprintMeasure(report, measure)} height={height} />;
}

/**
 * BurnupChart plots the work completed against the sprint's scope; the scope line steps up
 * when work is added and down when it is taken out. The guideline climbs to the commitment.
 */
export function BurnupChart({ report, measure, height }: { report: SprintReport; measure?: Measure; height?: number }) {
  return <SprintDayChart report={report} kind="burnup" measure={sprintMeasure(report, measure)} height={height} />;
}

type Line = { key: string; label: string; color: string; value: (p: BurndownPoint) => number };

function SprintDayChart({ report, kind, measure, height: H = 280 }: { report: SprintReport; kind: "burndown" | "burnup"; measure: Measure; height?: number }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const { sprint, burndown } = report;
  const m = measureOf(report, measure);
  const start = sprint.startDate ?? burndown[0]?.date;
  if (!start || burndown.length === 0) return null;
  const lastDate = burndown[burndown.length - 1].date;
  // The guideline reaches its target on the end date; an overrunning sprint extends the axis.
  const idealDays = Math.max(1, daysBetween(start, sprint.endDate && sprint.endDate > start ? sprint.endDate : lastDate));
  const days = Math.max(idealDays, daysBetween(start, lastDate));
  const x = linear([0, days], [M.left, width - M.right]);
  const lines: Line[] =
    kind === "burndown"
      ? [{ key: "remaining", label: "Remaining", color: C.series, value: m.remaining }]
      : [
          { key: "completed", label: "Completed", color: C.series, value: m.done },
          { key: "scope", label: "Scope", color: C.second, value: m.scope },
        ];
  const ideal = (i: number) => {
    const f = Math.min(1, i / idealDays);
    return kind === "burndown" ? Math.max(0, m.committed * (1 - f)) : m.committed * f;
  };
  const ticks = niceTicks(Math.max(m.committed, ...burndown.flatMap((p) => lines.map((l) => l.value(p))), 1), H < 240 ? 4 : 5, measure === "issues");
  const y = linear([0, ticks[ticks.length - 1]], [H - M.bottom, M.top]);
  const pts = burndown.map((p) => ({ p, i: daysBetween(start, p.date) }));
  const last = pts[pts.length - 1];

  // Each day's value holds until the next day's: step-after lines.
  const path = (l: Line) => pts.map(({ p, i }, k) => (k === 0 ? `M${x(i)},${y(l.value(p))}` : `H${x(i)}V${y(l.value(p))}`)).join("");
  const today = daysBetween(start, todayISO());
  const showToday = sprint.state === "active" && today >= 0 && today <= days;
  const step = Math.max(1, Math.ceil(64 / ((width - M.left - M.right) / days)));
  const xTicks = Array.from({ length: Math.floor(days / step) + 1 }, (_, k) => k * step);
  const guideEnd = kind === "burndown" ? 0 : m.committed;

  const at = (i: number) => pts.filter((q) => q.i <= i).pop()?.p;
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
  const hoveredPoint = hover != null && hover <= last.i ? at(hover) : undefined;
  const round = (n: number) => m.format(Math.round(n * 10) / 10);

  const endText = (key: string, v: number) => `${m.format(v)} ${key === "remaining" ? "left" : key === "completed" ? "done" : "scope"}`;
  // End labels: the primary line always; the scope too when it is clear of the first.
  const endLabels = lines
    .map((l, k) => ({ l, k, v: l.value(last.p) }))
    .filter((e, k, all) => k === 0 || Math.abs(y(e.v) - y(all[0].v)) >= 14);
  // Beside the dot when the days ahead leave room (the line ends there), else above it, or
  // below it near the top so it clears the "Today" label.
  const labelBeside = width - x(last.i) - 10 >= Math.max(...endLabels.map((e) => endText(e.l.key, e.v).length)) * 6.4;
  const summary =
    kind === "burndown"
      ? `Burndown: ${m.format(m.remaining(last.p))} of ${m.format(m.scope(last.p))} ${m.unit} remaining, ${m.format(m.committed)} committed`
      : `Burnup: ${m.format(m.done(last.p))} of ${m.format(m.scope(last.p))} ${m.unit} completed, ${m.format(m.committed)} committed`;

  return (
    <div>
      <div className="mb-2">
        <Legend items={[...lines.map((l) => ({ label: l.label, color: l.color, shape: "line" as const })), { label: "Guideline", color: C.reference, shape: "line" }]} />
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
                {m.format(t)}
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
          <polyline
            points={`${x(0)},${y(ideal(0))} ${x(idealDays)},${y(guideEnd)} ${x(days)},${y(guideEnd)}`}
            fill="none"
            stroke={C.reference}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />
          {[...lines].reverse().map((l) => (
            <path key={l.key} d={path(l)} fill="none" stroke={l.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          ))}
          {endLabels.map(({ l, v }) => (
            <g key={l.key}>
              <circle cx={x(last.i)} cy={y(v)} r={4} fill={l.color} stroke={C.surface} strokeWidth={2} />
              <text
                x={x(last.i) + (labelBeside ? 8 : -6)}
                y={y(v) + (labelBeside ? 0 : y(v) - M.top > 24 ? -9 : 17)}
                dy={labelBeside ? "0.32em" : undefined}
                textAnchor={labelBeside ? "start" : "end"}
                fontSize={11}
                fontWeight={600}
                fill="var(--color-ink)"
              >
                {endText(l.key, v)}
              </text>
            </g>
          ))}
          {hover != null && (
            <g pointerEvents="none">
              <line x1={x(hover)} x2={x(hover)} y1={M.top} y2={H - M.bottom} stroke={C.text} strokeWidth={1} />
              {hoveredPoint && lines.map((l) => <circle key={l.key} cx={x(hover)} cy={y(l.value(hoveredPoint))} r={4} fill={l.color} stroke={C.surface} strokeWidth={2} />)}
            </g>
          )}
          <rect x={M.left} y={M.top} width={Math.max(0, width - M.left - M.right)} height={H - M.top - M.bottom} fill="transparent" onPointerMove={move} onPointerLeave={() => setHover(null)} />
        </svg>
        {hover != null && (
          <Tooltip x={x(hover)} y={y(hoveredPoint ? lines[0].value(hoveredPoint) : ideal(hover))} width={width}>
            <p className="mb-1 font-medium text-ink">{formatDay(addDays(start, hover))}</p>
            {hoveredPoint && lines.map((l) => <TooltipRow key={l.key} color={l.color} value={`${m.format(l.value(hoveredPoint))} ${m.unit}`} label={l.label.toLowerCase()} />)}
            <TooltipRow color={C.reference} value={`${round(ideal(hover))} ${m.unit}`} label="guideline" />
          </Tooltip>
        )}
      </div>
      <DataTable
        head={["Date", ...lines.map((l) => l.label), "Guideline"]}
        rows={pts.map(({ p, i }) => [formatDay(p.date), ...lines.map((l) => m.format(l.value(p))), round(ideal(i))])}
      />
    </div>
  );
}
