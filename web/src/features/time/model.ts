import type { Schemas } from "@/api/client";

type TimeTracking = Schemas["TimeTracking"];
type Timesheet = Schemas["Timesheet"];

/*
 * Pure helpers for time tracking: the estimated / logged / remaining bar, the board card label,
 * date-time inputs, timesheet periods and the timesheet CSV.
 */

export type TimeBar = {
  /** Shares of the bar's width, 0–1. The bar spans the larger of the estimate and logged + remaining. */
  logged: number;
  remaining: number;
  /** Where the original estimate ends when work runs over it (else undefined). */
  estimateAt?: number;
  /** Logged + remaining beyond the original estimate, in seconds (0 when on track). */
  over: number;
  /** Nothing estimated or logged. */
  empty: boolean;
};

/** timeBar lays out the time tracking bar of an issue. */
export function timeBar(tt: TimeTracking | undefined): TimeBar {
  const original = tt?.originalSeconds;
  const spent = tt?.spentSeconds ?? 0;
  const remaining = tt?.remainingSeconds ?? 0;
  const projected = spent + remaining;
  const total = Math.max(original ?? 0, projected);
  if (total <= 0) return { logged: 0, remaining: 0, over: 0, empty: original == null && spent === 0 && tt?.remainingSeconds == null };
  const over = original != null ? Math.max(0, projected - original) : 0;
  return {
    logged: spent / total,
    remaining: remaining / total,
    estimateAt: over > 0 && original != null && original > 0 ? original / total : undefined,
    over,
    empty: false,
  };
}

export type BarSegment = { from: number; to: number; kind: "logged" | "logged-over" | "remaining" | "remaining-over" };

/**
 * barSegments cuts the bar into drawable pieces: work logged within the estimate, logged
 * beyond it, and the remaining estimate within and beyond it.
 */
export function barSegments(bar: TimeBar): BarSegment[] {
  const out: BarSegment[] = [];
  const add = (from: number, to: number, kind: BarSegment["kind"]) => to - from > 1e-9 && out.push({ from, to, kind });
  const end = bar.logged + bar.remaining;
  const e = bar.estimateAt ?? Infinity;
  add(0, Math.min(bar.logged, e), "logged");
  add(Math.min(bar.logged, e), bar.logged, "logged-over");
  add(bar.logged, Math.max(bar.logged, Math.min(end, e)), "remaining");
  add(Math.max(bar.logged, Math.min(end, e)), end, "remaining-over");
  return out;
}

/**
 * cardTimeLabel is the board card's time tracking: "3h / 1d" (logged of the original estimate),
 * else "5h left" or "2h logged"; null when nothing is tracked.
 */
export function cardTimeLabel(tt: TimeTracking | undefined, fmt: (s: number) => string): { text: string; title: string; over: boolean } | null {
  if (!tt) return null;
  const { originalSeconds: original, remainingSeconds: remaining, spentSeconds: spent } = tt;
  const rest = remaining != null ? `, ${fmt(remaining)} remaining` : "";
  if (original != null) {
    const over = spent + (remaining ?? 0) > original;
    return { text: `${fmt(spent)} / ${fmt(original)}`, title: `${fmt(spent)} logged of ${fmt(original)} estimated${rest}${over ? " (over the estimate)" : ""}`, over };
  }
  if (remaining != null) return { text: `${fmt(remaining)} left`, title: `${fmt(spent)} logged${rest}`, over: false };
  if (spent > 0) return { text: `${fmt(spent)} logged`, title: `${fmt(spent)} logged, no estimate`, over: false };
  return null;
}

/** autoRemaining previews the remaining estimate after logging `delta` more seconds (auto adjust). */
export function autoRemaining(remaining: number | undefined, delta: number): number | undefined {
  return remaining == null ? undefined : Math.max(0, remaining - delta);
}

const pad = (n: number) => String(n).padStart(2, "0");

/** toLocalInput formats an instant for <input type="datetime-local"> in the viewer's zone. */
export function toLocalInput(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** fromLocalInput reads a datetime-local value (viewer's zone) as an ISO instant; null when invalid. */
export function fromLocalInput(v: string): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(v);
  if (!m) return null;
  const d = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

// ---------------------------------------------------------------------------
// Timesheet periods (calendar dates, YYYY-MM-DD)

export type Period = "week" | "month";

const iso = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const parse = (s: string) => {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
};

/** periodRange is the Monday–Sunday week or the calendar month containing `day`. */
export function periodRange(period: Period, day: string): { from: string; to: string } {
  const d = parse(day);
  if (period === "month") return { from: iso(new Date(d.getFullYear(), d.getMonth(), 1)), to: iso(new Date(d.getFullYear(), d.getMonth() + 1, 0)) };
  const monday = new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7));
  return { from: iso(monday), to: iso(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + 6)) };
}

/** shiftPeriod moves a period's start by `n` weeks or months. */
export function shiftPeriod(period: Period, from: string, n: number): { from: string; to: string } {
  const d = parse(from);
  const moved = period === "month" ? new Date(d.getFullYear(), d.getMonth() + n, 1) : new Date(d.getFullYear(), d.getMonth(), d.getDate() + 7 * n);
  return periodRange(period, iso(moved));
}

/** isWeekend: Saturday or Sunday. */
export function isWeekend(day: string): boolean {
  const wd = parse(day).getDay();
  return wd === 0 || wd === 6;
}

/** hours is seconds as a number of hours to one decimal, "" for nothing (timesheet cells). */
export function hours(seconds: number): string {
  if (!seconds) return "";
  return String(Math.round((seconds / 3600) * 100) / 100);
}

function csvCell(v: string | number): string {
  const s = String(v);
  return /[",\n\r]/.test(s) || /^[=+\-@]/.test(s) ? `"${(/^[=+\-@]/.test(s) ? "'" : "") + s.replaceAll('"', '""')}"` : s;
}

/**
 * timesheetCsv writes the timesheet as CSV: a row per person and issue with hours per day, a
 * total row per person and a final total row. Values that a spreadsheet would run as a formula
 * are quoted and prefixed with an apostrophe.
 */
export function timesheetCsv(ts: Timesheet): string {
  const num = (s: number) => (s ? String(Math.round((s / 3600) * 100) / 100) : "0");
  const rows: (string | number)[][] = [["Person", "Email", "Issue", "Summary", ...ts.dates, "Total (hours)"]];
  for (const p of ts.people) {
    const name = p.user?.displayName ?? "Former user";
    const email = p.user?.email ?? "";
    for (const i of p.issues) rows.push([name, email, i.issue.key, i.issue.summary, ...i.days.map(num), num(i.total)]);
    rows.push([name, email, "", "Total", ...p.days.map(num), num(p.total)]);
  }
  rows.push(["Everyone", "", "", "Total", ...ts.dayTotals.map(num), num(ts.total)]);
  return rows.map((r) => r.map(csvCell).join(",")).join("\r\n") + "\r\n";
}
