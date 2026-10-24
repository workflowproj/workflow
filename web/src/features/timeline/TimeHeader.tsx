import { useMemo, type ReactNode } from "react";
import { cn } from "@/lib/cn";
import { header, type Zoom } from "./scale";
import { TREE_W } from "./TreeCell";

export const BAND_H = 26;
export const LANE_H = 24;

/**
 * TimeHeader is the sticky date header: months (or years, quarters) above days (or months),
 * an optional lane for sprint and release markers, and today's position.
 */
export function TimeHeader({
  range,
  zoom,
  x,
  ppd,
  width,
  today,
  lane,
}: {
  range: { start: number; end: number };
  zoom: Zoom;
  x: (day: number) => number;
  ppd: number;
  width: number;
  today: number;
  lane?: ReactNode;
}) {
  const h = useMemo(() => header(range, zoom), [range, zoom]);
  const todayIn = today >= range.start && today <= range.end;
  return (
    <div className="relative shrink-0 border-b border-line bg-surface" style={{ width, height: BAND_H * 2 + (lane ? LANE_H : 0) }}>
      <div className="absolute inset-x-0 top-0 border-b border-line" style={{ height: BAND_H }}>
        {h.top.map((s) => (
          <div key={s.start} className="absolute top-0 flex h-full items-center border-l border-line" style={{ left: x(s.start), width: (s.end - s.start + 1) * ppd }}>
            {/* The label sticks beside the tree while its period scrolls past. */}
            <span className="sticky truncate px-2 text-xs font-semibold text-ink" style={{ left: TREE_W }}>
              {s.label}
            </span>
          </div>
        ))}
      </div>
      <div className="absolute inset-x-0" style={{ top: BAND_H, height: BAND_H }}>
        {h.bottom.map((s) => {
          const isToday = zoom === "weeks" && s.start === today;
          return (
            <div
              key={s.start}
              className={cn(
                "absolute top-0 flex h-full items-center text-[11px] tabular-nums",
                zoom === "weeks" ? "justify-center" : "border-l border-line px-1.5",
                s.weekend ? "bg-surface-2 text-faint" : "text-muted",
                s.strong && "border-l border-line",
              )}
              style={{ left: x(s.start), width: (s.end - s.start + 1) * ppd }}
            >
              <span className={cn("truncate", isToday && "rounded-full bg-accent px-1.5 font-semibold text-accent-ink")}>{s.label}</span>
            </div>
          );
        })}
        {todayIn && zoom !== "weeks" && (
          <span className="absolute top-1/2 z-[1] -translate-x-1/2 -translate-y-1/2 rounded-full bg-accent px-1.5 text-[10px] leading-4 font-semibold text-accent-ink" style={{ left: x(today) + ppd / 2 }}>
            Today
          </span>
        )}
      </div>
      {lane && (
        <div className="absolute inset-x-0 border-t border-line" style={{ top: BAND_H * 2, height: LANE_H }}>
          {lane}
        </div>
      )}
    </div>
  );
}
