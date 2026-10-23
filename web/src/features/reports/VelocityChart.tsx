import { useState } from "react";
import { formatPoints } from "@/features/board/model";
import { barPath, C, DataTable, Legend, Tooltip, TooltipRow } from "./ChartBits";
import { linear, niceTicks, useElementWidth } from "./chart-utils";

export type VelocityPoint = { id: string; name: string; committed: number; completed: number; committedIssues: number; completedIssues: number };

const H = 260;
const M = { top: 20, right: 16, bottom: 34, left: 40 };
const GAP = 2;

/** VelocityChart compares committed and completed story points across completed sprints. */
export function VelocityChart({ data }: { data: VelocityPoint[] }) {
  const [ref, width] = useElementWidth<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const plotW = Math.max(0, width - M.left - M.right);
  const slot = plotW / Math.max(1, data.length);
  const bar = Math.max(4, Math.min(24, (slot - 20) / 2));
  const ticks = niceTicks(Math.max(1, ...data.flatMap((d) => [d.committed, d.completed])), 5);
  const y = linear([0, ticks[ticks.length - 1]], [H - M.bottom, M.top]);
  const average = data.length ? data.reduce((n, d) => n + d.completed, 0) / data.length : 0;
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
        <svg width={width} height={H} role="img" aria-label={`Velocity: average ${formatPoints(average)} points completed per sprint`} className="block overflow-visible">
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
                <path d={barPath(cx - GAP / 2 - bar, y(d.committed), bar, y(0) - y(d.committed))} fill={C.reference} />
                <path d={barPath(cx + GAP / 2, y(d.completed), bar, y(0) - y(d.completed))} fill={C.series} />
                <text x={cx + GAP / 2 + bar / 2} y={y(d.completed) - 6} textAnchor="middle" fontSize={11} fontWeight={600} fill="var(--color-ink)" className="tabular-nums">
                  {formatPoints(d.completed)}
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
                  aria-label={`${d.name}: ${formatPoints(d.completed)} of ${formatPoints(d.committed)} points completed`}
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
          <Tooltip x={center(hover) + bar} y={y(Math.max(h.committed, h.completed))} width={width}>
            <p className="mb-1 font-medium text-ink">{h.name}</p>
            <TooltipRow color={C.series} value={`${formatPoints(h.completed)} pts`} label={`completed (${h.completedIssues} issues)`} />
            <TooltipRow color={C.reference} value={`${formatPoints(h.committed)} pts`} label={`committed (${h.committedIssues} issues)`} />
          </Tooltip>
        )}
      </div>
      <DataTable head={["Sprint", "Committed", "Completed", "Issues done"]} rows={data.map((d) => [d.name, formatPoints(d.committed), formatPoints(d.completed), `${d.completedIssues} / ${d.committedIssues}`])} />
    </div>
  );
}
