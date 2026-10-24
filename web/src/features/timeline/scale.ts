import { weekday } from "./model";

/*
 * The time axis: pixels per day for each zoom, the visible range, the two header bands and
 * the vertical grid lines. Days are the integers from model.ts.
 */

export type Zoom = "weeks" | "months" | "quarters";

export const ZOOMS: { id: Zoom; label: string; ppd: number }[] = [
  { id: "weeks", label: "Weeks", ppd: 30 },
  { id: "months", label: "Months", ppd: 8 },
  { id: "quarters", label: "Quarters", ppd: 2.5 },
];

export const ppdFor = (z: Zoom) => ZOOMS.find((x) => x.id === z)!.ppd;

const ymd = (day: number) => {
  const d = new Date(day * 86_400_000);
  return { y: d.getUTCFullYear(), m: d.getUTCMonth(), d: d.getUTCDate() };
};
const monthStart = (y: number, m: number) => Math.round(Date.UTC(y, m, 1) / 86_400_000);

export function startOfMonth(day: number) {
  const { y, m } = ymd(day);
  return monthStart(y, m);
}

/** addMonths moves to the first day of the month `n` months away. */
export function addMonths(day: number, n: number) {
  const { y, m } = ymd(day);
  return monthStart(y, m + n);
}

/**
 * timeRange covers every date drawn plus some room around today: from the start of the month
 * before the earliest date to the end of the month after the latest (at least 3 months ahead).
 */
export function timeRange(days: number[], today: number): { start: number; end: number } {
  const lo = Math.min(today - 30, ...days);
  const hi = Math.max(today + 90, ...days);
  return { start: addMonths(lo, -1), end: addMonths(hi, 2) - 1 };
}

export type Segment = { start: number; end: number; label: string; weekend?: boolean; strong?: boolean };

const monthLong = new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric", timeZone: "UTC" });
const monthShort = new Intl.DateTimeFormat(undefined, { month: "short", timeZone: "UTC" });

function months(range: { start: number; end: number }, label: (day: number) => string): Segment[] {
  const out: Segment[] = [];
  for (let s = startOfMonth(range.start); s <= range.end; s = addMonths(s, 1)) {
    const e = addMonths(s, 1) - 1;
    out.push({ start: Math.max(s, range.start), end: Math.min(e, range.end), label: label(s) });
  }
  return out;
}

/** header returns the top and bottom header bands for a zoom (end days are inclusive). */
export function header(range: { start: number; end: number }, zoom: Zoom): { top: Segment[]; bottom: Segment[] } {
  const short = (day: number) => monthShort.format(new Date(day * 86_400_000));
  if (zoom === "weeks") {
    const bottom: Segment[] = [];
    for (let d = range.start; d <= range.end; d++) {
      const wd = weekday(d);
      bottom.push({ start: d, end: d, label: String(ymd(d).d), weekend: wd === 0 || wd === 6, strong: wd === 1 });
    }
    return { top: months(range, (d) => monthLong.format(new Date(d * 86_400_000))), bottom };
  }
  const bottom = months(range, short);
  if (zoom === "months") {
    const years: Segment[] = [];
    for (const m of bottom) {
      const y = String(ymd(m.start).y);
      const last = years[years.length - 1];
      if (last?.label === y) last.end = m.end;
      else years.push({ start: m.start, end: m.end, label: y });
    }
    return { top: years, bottom };
  }
  const quarters: Segment[] = [];
  for (const m of bottom) {
    const { y, m: mo } = ymd(m.start);
    const label = `Q${Math.floor(mo / 3) + 1} ${y}`;
    const last = quarters[quarters.length - 1];
    if (last?.label === label) last.end = m.end;
    else quarters.push({ start: m.start, end: m.end, label });
  }
  return { top: quarters, bottom };
}

/** gridLines are the vertical lines behind the rows: Mondays in week zoom, month starts otherwise. */
export function gridLines(range: { start: number; end: number }, zoom: Zoom): { day: number; strong: boolean }[] {
  const out: { day: number; strong: boolean }[] = [];
  if (zoom === "weeks") {
    for (let d = range.start; d <= range.end; d++) if (weekday(d) === 1) out.push({ day: d, strong: ymd(d).d <= 7 });
    return out;
  }
  for (let s = addMonths(range.start, 1); s <= range.end; s = addMonths(s, 1)) out.push({ day: s, strong: zoom === "months" || ymd(s).m % 3 === 0 });
  return out;
}

/** snapDays turns a pointer distance into whole days. */
export const snapDays = (px: number, ppd: number) => Math.round(px / ppd);

