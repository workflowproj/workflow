/*
 * Work durations the way people write them in a tracker: "1w 2d 3h 30m", where a day and a week
 * are the working day and week the server is configured with (WFM_TIME_HOURS_PER_DAY,
 * WFM_TIME_DAYS_PER_WEEK; GET /time-tracking/settings). Durations travel as seconds. The Go side
 * (internal/timetrack) follows the same rules; keep the two in step.
 */

export type TimeUnits = { hoursPerDay: number; daysPerWeek: number };

export const DEFAULT_UNITS: TimeUnits = { hoursPerDay: 8, daysPerWeek: 5 };

const dayMinutes = (u: TimeUnits) => Math.max(Math.round(u.hoursPerDay * 60), 1);
const weekMinutes = (u: TimeUnits) => Math.max(Math.round(u.hoursPerDay * u.daysPerWeek * 60), 1);

const BARE = /^\d+(?:[.,]\d+)?$/;
// Longer unit names first so "2 hours" is not read as "2 h" followed by "ours".
const PART = /^(\d+(?:[.,]\d+)?)\s*(weeks|week|wks|wk|w|days|day|d|hours|hour|hrs|hr|h|minutes|minute|mins|min|m)/;

export type ParseResult = { seconds: number; error?: undefined } | { seconds?: undefined; error: string };

/**
 * parseDuration reads "1w 2d 3h 30m", "1.5h", "90m" or "2 days" into seconds. A bare number is
 * minutes; units may come in any order and add up.
 */
export function parseDuration(text: string, units: TimeUnits = DEFAULT_UNITS): ParseResult {
  const s = text.trim().toLowerCase();
  if (!s) return { error: "Enter a duration such as 2h 30m" };
  const num = (v: string) => Number(v.replace(",", "."));
  if (BARE.test(s)) return { seconds: Math.round(num(s) * 60) };
  let total = 0;
  let rest = s;
  while (rest) {
    const m = PART.exec(rest);
    if (!m) return { error: `"${text.trim()}" isn't a duration — use w, d, h and m, e.g. 1d 4h or 90m` };
    const n = num(m[1]);
    const unit = m[2][0];
    total += unit === "w" ? n * weekMinutes(units) * 60 : unit === "d" ? n * dayMinutes(units) * 60 : unit === "h" ? n * 3600 : n * 60;
    rest = rest.slice(m[0].length).replace(/^[\s,+]+/, "");
  }
  return { seconds: Math.round(total) };
}

/** formatDuration writes seconds as "1w 2d 3h 30m" (to the minute, zero parts left out); nothing is "0m". */
export function formatDuration(seconds: number | null | undefined, units: TimeUnits = DEFAULT_UNITS): string {
  let mins = Math.round((seconds ?? 0) / 60);
  if (mins <= 0) return "0m";
  const parts: string[] = [];
  const week = weekMinutes(units);
  const day = dayMinutes(units);
  const w = Math.floor(mins / week);
  if (w > 0) {
    parts.push(`${w}w`);
    mins -= w * week;
  }
  const d = Math.floor(mins / day);
  if (d > 0) {
    parts.push(`${d}d`);
    mins -= d * day;
  }
  const h = Math.floor(mins / 60);
  if (h > 0) {
    parts.push(`${h}h`);
    mins -= h * 60;
  }
  if (mins > 0) parts.push(`${mins}m`);
  return parts.join(" ");
}

/** formatHours is a compact hour count for charts and tables: "12.5h", "0h". */
export function formatHours(seconds: number): string {
  const h = Math.round((seconds / 3600) * 10) / 10;
  return `${h}h`;
}
