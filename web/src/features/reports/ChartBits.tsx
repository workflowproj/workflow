import type { ReactNode } from "react";
import type { StatusCategory } from "@/api/types";
import { cn } from "@/lib/cn";

/*
 * Small pieces shared by the hand-drawn SVG charts. Colours come from the theme tokens so
 * charts follow light and dark mode: the main series is the accent, the reference series
 * (commitment, guideline) is the faint grey, gridlines are the hairline colour.
 */

export const C = {
  series: "var(--color-accent)",
  reference: "var(--color-faint)",
  grid: "var(--color-line)",
  axis: "var(--color-line-strong)",
  text: "var(--color-muted)",
  surface: "var(--color-surface)",
  /** A second series beside the accent one, e.g. a sprint's scope on a burnup. */
  second: "var(--color-chart-2)",
  done: "var(--color-chart-done)",
};

/**
 * Categorical slots in their fixed order (see index.css); a ninth series folds into "Other".
 * Spelled out: Tailwind only emits the theme variables it finds named in the source.
 */
export const CATEGORICAL = [
  "var(--color-chart-1)",
  "var(--color-chart-2)",
  "var(--color-chart-3)",
  "var(--color-chart-4)",
  "var(--color-chart-5)",
  "var(--color-chart-6)",
  "var(--color-chart-7)",
  "var(--color-chart-8)",
];

/** Status categories keep their meaning in charts: grey to do, blue in progress, green done. */
export const categoryColor: Record<StatusCategory, string> = {
  todo: "var(--color-chart-todo)",
  in_progress: "var(--color-chart-progress)",
  done: "var(--color-chart-done)",
};

/** Tooltip floats inside a `relative` chart container at (x, y), flipping left near the right edge. */
export function Tooltip({ x, y, width, children }: { x: number; y: number; width: number; children: ReactNode }) {
  const flip = x > width - 180;
  return (
    <div
      role="status"
      className="pointer-events-none absolute z-10 min-w-36 rounded-md border border-line bg-surface px-2.5 py-2 text-xs shadow-lg"
      style={{ left: flip ? undefined : x + 12, right: flip ? width - x + 12 : undefined, top: Math.max(0, y - 12) }}
    >
      {children}
    </div>
  );
}

/** TooltipRow: the value leads, the series name follows, keyed by a short line in the series colour. */
export function TooltipRow({ color, label, value }: { color?: string; label: ReactNode; value: ReactNode }) {
  return (
    <div className="flex items-center gap-2 py-0.5">
      {color && <span aria-hidden className="h-0.5 w-3 shrink-0 rounded-full" style={{ background: color }} />}
      <span className="font-semibold text-ink tabular-nums">{value}</span>
      <span className="text-muted">{label}</span>
    </div>
  );
}

export function Legend({ items }: { items: { label: string; color: string; shape: "line" | "rect" }[] }) {
  return (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
      {items.map((i) => (
        <li key={i.label} className="inline-flex items-center gap-1.5">
          <span aria-hidden className={cn("shrink-0", i.shape === "line" ? "h-0.5 w-4 rounded-full" : "size-2.5 rounded-[3px]")} style={{ background: i.color }} />
          {i.label}
        </li>
      ))}
    </ul>
  );
}

export function StatTile({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="rounded-lg border border-line bg-surface px-4 py-3">
      <p className="text-xs text-muted">{label}</p>
      <p className="mt-1 text-2xl font-semibold tracking-tight">{value}</p>
      {sub && <p className="mt-0.5 text-xs text-muted">{sub}</p>}
    </div>
  );
}

/** TruncatedNote says a report counted only the newest issues (the server reads at most 100,000). */
export function TruncatedNote({ what }: { what: "created" | "resolved" }) {
  return <p className="mt-2 text-xs text-muted">Based on the 100,000 most recently {what} issues. Narrow the projects or query to count them all.</p>;
}

/** Segmented is a small single-choice switch (a radio group), e.g. points or issues. */
export function Segmented<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: { value: T; label: string }[]; onChange: (v: T) => void }) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex rounded-md border border-line-strong bg-surface p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "h-6 rounded px-2 text-xs font-medium focus-visible:outline-2 focus-visible:outline-accent",
            value === o.value ? "bg-accent-soft text-accent" : "text-muted hover:text-ink",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** DataTable is the chart's table view, collapsed by default. */
export function DataTable({ head, rows }: { head: string[]; rows: ReactNode[][] }) {
  return (
    <details className="group mt-3 text-sm">
      <summary className="cursor-pointer text-xs font-medium text-muted select-none hover:text-ink">Show data</summary>
      <div className="mt-2 max-h-64 overflow-auto rounded-md border border-line">
        <table className="w-full border-collapse text-xs [&_td]:border-t [&_td]:border-line [&_td]:px-3 [&_td]:py-1.5 [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left [&_th]:font-semibold [&_th]:text-muted">
          <thead className="sticky top-0 bg-surface-2">
            <tr>
              {head.map((h) => (
                <th key={h}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody className="tabular-nums">
            {rows.map((r, i) => (
              <tr key={i}>
                {r.map((c, j) => (
                  <td key={j}>{c}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  );
}

/** barPath draws a column with a 4px rounded top and a square base. */
export function barPath(x: number, y: number, w: number, h: number, r = 4) {
  if (h <= 0 || w <= 0) return "";
  const k = Math.min(r, h, w / 2);
  return `M${x},${y + h}V${y + k}Q${x},${y} ${x + k},${y}H${x + w - k}Q${x + w},${y} ${x + w},${y + k}V${y + h}Z`;
}
