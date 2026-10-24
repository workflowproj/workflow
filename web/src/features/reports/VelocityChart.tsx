import { useState } from "react";
import { formatPoints } from "@/features/board/model";
import { barPath, C, DataTable, Legend, Tooltip, TooltipRow } from "./ChartBits";
import { linear, niceTicks, useElementWidth } from "./chart-utils";

export type VelocityPoint = { id: string; name: string; committed: number; completed: number; committedIssues: number; completedIssues: number };

const M = { top: 20, right: 16, bottom: 34, left: 40 };
const GAP = 2;

/**
 * VelocityChart compares committed and completed work across completed sprints, in story
 * points or (`unit="issues"`) issue counts. `average` overrides the mean of the bars shown.
 */
export function VelocityChart({ data, unit = "points", average: avg, height: H = 260 }: { data: VelocityPoint[]; unit?: "points" | "issues"; average?: number; height?: number }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const pts = unit === "points";
  const committedOf = (d: VelocityPoint) => (pts ? d.committed : d.committedIssues);
  const completedOf = (d: VelocityPoint) => (pts ? d.completed : d.completedIssues);
  const u = pts ? "points" : "issues";
  const plotW = Math.max(0, width - M.left - M.right);
  const slot = plotW / Math.max(1, data.length);
  const bar = Math.max(4, Math.min(24, (slot - 20) / 2));
  const ticks = niceTicks(Math.max(1, ...data.flatMap((d) => [committedOf(d), completedOf(d)])), H < 240 ? 4 : 5);
  const y = linear([0, ticks[ticks.length - 1]], [H - M.bottom, M.top]);
  const average = avg ?? (data.length ? data.reduce((n, d) => n + completedOf(d), 0) / data.length : 0);
  const center = (i: number) => M.left + slot * (i + 0.5);
  const maxChars = Math.max(4, Math.floor(slot / 6.5));
  const short = (s: string) => (s.length > maxChars ? `${s.slice(0, maxChars - 1)}…` : s);
  const h = hover != null ? data[hover] : null;

  return (
    <div>
      <div className="mb-2">
        <Legend
          items={[
            { label: "Committed", color: C.reference, shape: "rect" },
            { label: "Completed", color: C.series, shape: "rect" },
          ]}
        />
      </div>
      <div ref={ref} className="relative">
        <svg width={width} height={H} role="img" aria-label={`Velocity: average ${formatPoints(Math.round(average * 10) / 10)} ${u} completed per sprint`} className="block overflow-visible">
          {ticks.map((t) => (
            <g key={t}>
              <line x1={M.left} x2={width - M.right} y1={y(t)} y2={y(t)} stroke={t === 0 ? C.axis : C.grid} strokeWidth={1} />
              <text x={M.left - 8} y={y(t)} dy="0.32em" textAnchor="end" fontSize={11} fill={C.text} className="tabular-nums">
                {formatPoints(t)}
              </text>
            </g>
          ))}
          {data.map((d, i) => {
            const cx = center(i);
            return (
              <g key={d.id} opacity={hover == null || hover === i ? 1 : 0.55}>
                <path d={barPath(cx - GAP / 2 - bar, y(committedOf(d)), bar, y(0) - y(committedOf(d)))} fill={C.reference} />
                <path d={barPath(cx + GAP / 2, y(completedOf(d)), bar, y(0) - y(completedOf(d)))} fill={C.series} />
                <text x={cx + GAP / 2 + bar / 2} y={y(completedOf(d)) - 6} textAnchor="middle" fontSize={11} fontWeight={600} fill="var(--color-ink)" className="tabular-nums">
                  {formatPoints(completedOf(d))}
                </text>
                <text x={cx} y={H - M.bottom + 18} textAnchor="middle" fontSize={11} fill={C.text}>
                  <title>{d.name}</title>
                  {short(d.name)}
                </text>
                <rect
                  x={cx - slot / 2}
                  y={M.top}
                  width={slot}
                  height={H - M.top - M.bottom}
                  fill="transparent"
                  tabIndex={0}
                  role="img"
                  aria-label={`${d.name}: ${formatPoints(completedOf(d))} of ${formatPoints(committedOf(d))} ${u} completed`}
                  className="outline-none focus-visible:stroke-accent"
                  onPointerEnter={() => setHover(i)}
                  onPointerLeave={() => setHover(null)}
                  onFocus={() => setHover(i)}
                  onBlur={() => setHover(null)}
                />
              </g>
            );
          })}
          {data.length > 1 && (
            <g pointerEvents="none">
              <line x1={M.left} x2={width - M.right} y1={y(average)} y2={y(average)} stroke={C.text} strokeWidth={1} />
              <text x={width - M.right} y={y(average) - 5} textAnchor="end" fontSize={11} fill={C.text}>
                Average {formatPoints(Math.round(average * 10) / 10)}
              </text>
            </g>
          )}
        </svg>
        {h && hover != null && (
          <Tooltip x={center(hover) + bar} y={y(Math.max(committedOf(h), completedOf(h)))} width={width}>
            <p className="mb-1 font-medium text-ink">{h.name}</p>
            {pts ? (
              <>
                <TooltipRow color={C.series} value={`${formatPoints(h.completed)} pts`} label={`completed (${h.completedIssues} issues)`} />
                <TooltipRow color={C.reference} value={`${formatPoints(h.committed)} pts`} label={`committed (${h.committedIssues} issues)`} />
              </>
            ) : (
              <>
                <TooltipRow color={C.series} value={`${h.completedIssues} issues`} label={`completed (${formatPoints(h.completed)} pts)`} />
                <TooltipRow color={C.reference} value={`${h.committedIssues} issues`} label={`committed (${formatPoints(h.committed)} pts)`} />
              </>
            )}
          </Tooltip>
        )}
      </div>
      <DataTable
        head={["Sprint", "Committed points", "Completed points", "Issues done"]}
        rows={data.map((d) => [d.name, formatPoints(d.committed), formatPoints(d.completed), `${d.completedIssues} / ${d.committedIssues}`])}
      />
    </div>
  );
}
