import { useLayoutEffect, useRef, useState } from "react";

/**
 * niceTicks returns 0..max in round steps (1, 2, 2.5, 5 × 10^n), about `count` of them.
 * `integer` keeps steps whole for counts (no 2.5 issues).
 */
export function niceTicks(max: number, count = 5, integer = false): number[] {
  if (!(max > 0)) return [0, 1];
  const raw = max / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  let step = ([1, 2, 2.5, 5, 10].find((m) => m * mag >= raw) ?? 10) * mag;
  if (integer) step = step < 1 ? 1 : step === 2.5 ? 2 : step === 25 ? 20 : step;
  const top = Math.ceil(max / step - 1e-9) * step;
  const out: number[] = [];
  for (let v = 0; v <= top + step / 2; v += step) out.push(Math.round(v * 1000) / 1000);
  return out;
}

/** linear maps a domain onto a range. */
export function linear([d0, d1]: [number, number], [r0, r1]: [number, number]) {
  const k = d1 === d0 ? 0 : (r1 - r0) / (d1 - d0);
  return (v: number) => r0 + (v - d0) * k;
}

/** durationUnit picks hours or days for an axis whose largest value is `maxHours`. */
export function durationUnit(maxHours: number): "h" | "d" {
  return maxHours < 48 ? "h" : "d";
}

/** formatDuration renders hours as "45m", "5.5h" or "3.2d". */
export function formatDuration(hours: number, unit: "h" | "d" = durationUnit(hours)): string {
  if (unit === "d") return `${trim(hours / 24)}d`;
  if (hours > 0 && hours < 1) return `${Math.max(1, Math.round(hours * 60))}m`;
  return `${trim(hours)}h`;
}

function trim(n: number) {
  return n >= 10 ? String(Math.round(n)) : String(Math.round(n * 10) / 10);
}

/** useElementWidth measures a container so SVG charts can draw at their real width. */
export function useElementWidth<T extends HTMLElement>(fallback = 640) {
  const ref = useRef<T>(null);
  const [width, setWidth] = useState(fallback);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setWidth(el.clientWidth || fallback);
    const ro = new ResizeObserver(([e]) => setWidth(Math.round(e.contentRect.width) || fallback));
    ro.observe(el);
    return () => ro.disconnect();
  }, [fallback]);
  return [ref, width] as const;
}
