/*
 * Calendar-date helpers for sprints and releases. The API uses plain YYYY-MM-DD dates; these
 * work in local time so "today" matches the viewer's calendar.
 */

const pad = (n: number) => String(n).padStart(2, "0");

export function toISODate(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function parseISODate(iso: string): Date {
  const [y, m, d] = iso.slice(0, 10).split("-").map(Number);
  return new Date(y, m - 1, d);
}

export function todayISO(): string {
  return toISODate(new Date());
}

export function addDays(iso: string, n: number): string {
  const d = parseISODate(iso);
  d.setDate(d.getDate() + n);
  return toISODate(d);
}

/** daysBetween counts calendar days from a to b (negative when b is earlier). */
export function daysBetween(a: string, b: string): number {
  return Math.round((parseISODate(b).getTime() - parseISODate(a).getTime()) / 86_400_000);
}

const short = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const withYear = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" });

/** formatDay renders "Oct 19" (with the year when it isn't this year). */
export function formatDay(iso: string): string {
  const d = parseISODate(iso);
  return (d.getFullYear() === new Date().getFullYear() ? short : withYear).format(d);
}

/** formatRange renders "Oct 19 – Nov 2". */
export function formatRange(start?: string, end?: string): string {
  if (!start && !end) return "";
  if (start && end) return `${formatDay(start)} – ${formatDay(end)}`;
  return start ? `From ${formatDay(start)}` : `Until ${formatDay(end!)}`;
}

/** remainingLabel says how long an active sprint has left ("3 days left", "Ends today", "2 days overdue"). */
export function remainingLabel(end: string | undefined, today = todayISO()): { text: string; overdue: boolean } | null {
  if (!end) return null;
  const n = daysBetween(today, end);
  if (n > 1) return { text: `${n} days left`, overdue: false };
  if (n === 1) return { text: "1 day left", overdue: false };
  if (n === 0) return { text: "Ends today", overdue: false };
  return { text: `${-n} day${n === -1 ? "" : "s"} overdue`, overdue: true };
}
