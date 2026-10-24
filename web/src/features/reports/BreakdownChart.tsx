import { useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import type { StatusCategory, User } from "@/api/types";
import { categoryLabel } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { C, CATEGORICAL, categoryColor, DataTable } from "./ChartBits";
import { arcPath, foldGroups } from "./series";

/** One group of a breakdown, already in the measure shown (issues or points). */
export type BreakdownRow = { key: string; label: string; value: number; category?: StatusCategory; user?: User };

type Props = {
  rows: BreakdownRow[];
  total: number;
  /** "issues" or "pts", after each value. */
  unit: string;
  format: (n: number) => string;
  /** Colour rows by status category instead of the accent. */
  byCategory?: boolean;
  /** Avatars beside people. */
  people?: boolean;
  /** JQL for a group's issues; rows become links to search. */
  jqlFor?: (row: BreakdownRow) => string | undefined;
  label: string;
};

type Shown = BreakdownRow & { other?: boolean; count?: number };

const pct = (v: number, total: number) => (total > 0 ? Math.round((v / total) * 100) : 0);

function RowLink({ jql, children, className, ...rest }: { jql?: string; children: ReactNode; className?: string } & React.HTMLAttributes<HTMLElement>) {
  if (!jql) return <div className={className} {...rest}>{children}</div>;
  return (
    <Link to="/issues" search={{ jql }} className={cn(className, "hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent")} {...rest}>
      {children}
    </Link>
  );
}

/**
 * BreakdownBars lists groups as horizontal bars from one baseline, value at the tip. One series,
 * one colour (the accent), unless the groups are statuses, whose category colours carry meaning.
 */
export function BreakdownBars({ rows, total, unit, format, byCategory, people, jqlFor, label }: Props) {
  const shown: Shown[] = foldGroups(rows, 8);
  const max = Math.max(1, ...shown.map((r) => r.value));
  return (
    <div>
      <ul aria-label={label} className="flex flex-col">
        {shown.map((r) => {
          const color = byCategory && r.category ? categoryColor[r.category] : r.other ? C.reference : C.series;
          const name = r.key === "" ? r.label : r.label || "–";
          return (
            <li key={r.key}>
              <RowLink
                jql={r.other ? undefined : jqlFor?.(r)}
                aria-label={`${name}: ${format(r.value)} ${unit} (${pct(r.value, total)}%)`}
                className="grid grid-cols-[minmax(5.5rem,38%)_1fr_auto] items-center gap-3 rounded-md px-1.5 py-1.5 text-[13px]"
              >
                <span className={cn("flex min-w-0 items-center gap-2", (r.key === "" || r.other) && "text-muted")}>
                  {people && !r.other && <UserAvatar user={r.user ?? null} size={20} />}
                  <span className="truncate" title={name}>
                    {name}
                  </span>
                </span>
                <span className="h-2" aria-hidden>
                  <span className="block h-full rounded-r-[4px]" style={{ width: r.value > 0 ? `max(2px, ${(r.value / max) * 100}%)` : 0, background: color }} />
                </span>
                <span className="text-right tabular-nums">
                  <span className="font-semibold">{format(r.value)}</span>
                  <span className="ml-1.5 inline-block w-9 text-xs text-muted">{pct(r.value, total)}%</span>
                </span>
              </RowLink>
            </li>
          );
        })}
      </ul>
      <DataTable head={["Group", unit === "pts" ? "Points" : "Issues", "Share"]} rows={rows.map((r) => [r.label, format(r.value), `${pct(r.value, total)}%`])} />
    </div>
  );
}

const SIZE = 148;
const R = SIZE / 2;
const INNER = R - 22;

/**
 * BreakdownDonut shows part-to-whole for up to six groups (the rest fold into "Other"), with
 * the total in the middle. Hovering or focusing a legend row lifts its segment.
 */
export function BreakdownDonut({ rows, total, unit, format, byCategory, people, jqlFor, label }: Props) {
  const [active, setActive] = useState<string | null>(null);
  const shown: Shown[] = foldGroups(rows, 7);
  const sum = shown.reduce((n, r) => n + r.value, 0);
  let slot = 0;
  const colored = shown.map((r) => ({ ...r, color: r.other ? C.reference : byCategory && r.category ? categoryColor[r.category] : CATEGORICAL[slot++ % CATEGORICAL.length] }));
  let a = 0;
  const arcs = colored.map((r) => {
    const a0 = a;
    a += sum > 0 ? (r.value / sum) * Math.PI * 2 : 0;
    return { ...r, a0, a1: a };
  });
  const focus = colored.find((r) => r.key === active);
  return (
    <div>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
        <svg width={SIZE} height={SIZE} role="img" aria-label={`${label}: ${colored.map((r) => `${r.label} ${format(r.value)}`).join(", ")}`} className="shrink-0">
          {sum === 0 && <circle cx={R} cy={R} r={(R + INNER) / 2} fill="none" stroke={C.grid} strokeWidth={R - INNER} />}
          {arcs.map((r) =>
            r.a1 - r.a0 > 0 ? (
              <path
                key={r.key}
                d={arcPath(R, R, active === r.key ? R : R - 2, INNER, r.a0, r.a1)}
                fill={r.color}
                stroke={C.surface}
                strokeWidth={2}
                strokeLinejoin="round"
                opacity={active && active !== r.key ? 0.45 : 1}
                onPointerEnter={() => setActive(r.key)}
                onPointerLeave={() => setActive(null)}
              />
            ) : null,
          )}
          <text x={R} y={R - 4} textAnchor="middle" fontSize={20} fontWeight={600} fill="var(--color-ink)">
            {format(focus ? focus.value : total)}
          </text>
          <text x={R} y={R + 14} textAnchor="middle" fontSize={11} fill={C.text}>
            {focus ? `${pct(focus.value, total)}% · ${focus.label.length > 14 ? `${focus.label.slice(0, 13)}…` : focus.label}` : unit}
          </text>
        </svg>
        <ul aria-label={label} className="flex min-w-40 flex-1 flex-col">
          {colored.map((r) => (
            <li key={r.key}>
              <RowLink
                jql={r.other ? undefined : jqlFor?.(r)}
                aria-label={`${r.label}: ${format(r.value)} ${unit} (${pct(r.value, total)}%)`}
                onPointerEnter={() => setActive(r.key)}
                onPointerLeave={() => setActive(null)}
                onFocus={() => setActive(r.key)}
                onBlur={() => setActive(null)}
                className="flex items-center gap-2 rounded-md px-1.5 py-1 text-[13px]"
              >
                <span aria-hidden className="size-2.5 shrink-0 rounded-[3px]" style={{ background: r.color }} />
                {people && !r.other && <UserAvatar user={r.user ?? null} size={18} />}
                <span className={cn("min-w-0 flex-1 truncate", (r.key === "" || r.other) && "text-muted")}>{r.label}</span>
                <span className="font-semibold tabular-nums">{format(r.value)}</span>
                <span className="w-9 text-right text-xs text-muted tabular-nums">{pct(r.value, total)}%</span>
              </RowLink>
            </li>
          ))}
        </ul>
      </div>
      <DataTable head={["Group", unit === "pts" ? "Points" : "Issues", "Share"]} rows={rows.map((r) => [r.label, format(r.value), `${pct(r.value, total)}%`])} />
    </div>
  );
}

const ORDER: StatusCategory[] = ["done", "in_progress", "todo"];

/**
 * CategoryBar is a single stacked bar of work by status category (done first, so progress
 * reads left to right), with 2px surface gaps between segments and a legend with the values.
 */
export function CategoryBar({ values, unit, format }: { values: Partial<Record<StatusCategory, number>>; unit: string; format: (n: number) => string }) {
  const total = ORDER.reduce((n, c) => n + (values[c] ?? 0), 0);
  const parts = ORDER.filter((c) => (values[c] ?? 0) > 0);
  return (
    <div>
      <div
        role="img"
        aria-label={ORDER.map((c) => `${format(values[c] ?? 0)} ${unit} ${categoryLabel[c].toLowerCase()}`).join(", ")}
        className="flex h-2.5 w-full gap-0.5 overflow-hidden rounded-[4px]"
        style={{ background: total === 0 ? C.grid : undefined }}
      >
        {parts.map((c) => (
          <span key={c} className="h-full" style={{ flexGrow: values[c], flexBasis: 0, background: categoryColor[c] }} />
        ))}
      </div>
      <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        {ORDER.map((c) => (
          <li key={c} className="inline-flex items-center gap-1.5">
            <span aria-hidden className="size-2.5 rounded-[3px]" style={{ background: categoryColor[c] }} />
            <span className="font-semibold text-ink tabular-nums">{format(values[c] ?? 0)}</span> {categoryLabel[c].toLowerCase()}
          </li>
        ))}
      </ul>
    </div>
  );
}
