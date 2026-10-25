import type { Schemas } from "@/api/client";

/*
 * What board cards show besides the summary, type icon and key (Board.cardFields), and the
 * small formatting rules behind the optional fields.
 */

export type CardField = Schemas["CardField"];

export const CARD_FIELDS: { field: CardField; label: string; hint: string }[] = [
  { field: "epic", label: "Epic", hint: "The epic, or a sub-task's parent" },
  { field: "labels", label: "Labels", hint: "Up to three, then a count" },
  { field: "priority", label: "Priority", hint: "" },
  { field: "dev", label: "Development", hint: "Pull requests, branches and the latest pipeline" },
  { field: "points", label: "Story points", hint: "" },
  { field: "assignee", label: "Assignee", hint: "" },
  { field: "dueDate", label: "Due date", hint: "Red once overdue" },
  { field: "daysInColumn", label: "Days in column", hint: "Time since the issue entered its status" },
  { field: "timeTracking", label: "Time tracking", hint: "Time logged of the estimate, or what is left; red when over" },
];

export const DEFAULT_CARD_FIELDS: CardField[] = ["epic", "labels", "priority", "dev", "points", "assignee"];

const known = new Set<string>(CARD_FIELDS.map((f) => f.field));

/** normalizeCardFields drops unknown and repeated fields; a missing list means the defaults. */
export function normalizeCardFields(fields: readonly string[] | null | undefined): CardField[] {
  if (!fields) return [...DEFAULT_CARD_FIELDS];
  return [...new Set(fields)].filter((f): f is CardField => known.has(f));
}

/** toggleCardField shows a field (at the end) or hides it. */
export function toggleCardField(fields: CardField[], field: CardField, on: boolean): CardField[] {
  const rest = fields.filter((f) => f !== field);
  return on ? [...rest, field] : rest;
}

/** moveCardField moves a shown field to position `to` (clamped). */
export function moveCardField(fields: CardField[], field: CardField, to: number): CardField[] {
  const from = fields.indexOf(field);
  if (from < 0) return fields;
  const out = fields.filter((f) => f !== field);
  out.splice(Math.max(0, Math.min(to, out.length)), 0, field);
  return out;
}

const tagFields = new Set<CardField>(["epic", "labels"]);
const trailing = new Set<CardField>(["points", "assignee"]);

/**
 * cardLayout splits the fields into the card's zones, each in the configured order: epic and
 * labels under the summary; the rest in the footer after the key, where a final run of points
 * and assignee sits at the right edge (the familiar layout for the default order).
 */
export function cardLayout(fields: CardField[]): { tags: CardField[]; left: CardField[]; right: CardField[] } {
  const tags = fields.filter((f) => tagFields.has(f));
  const footer = fields.filter((f) => !tagFields.has(f));
  let split = footer.length;
  while (split > 0 && trailing.has(footer[split - 1])) split--;
  return { tags, left: footer.slice(0, split), right: footer.slice(split) };
}

const DAY = 86_400_000;

/** daysSince counts whole days from an instant to `now` (never negative); undefined without a time. */
export function daysSince(iso: string | undefined, now: number): number | undefined {
  if (!iso) return undefined;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return undefined;
  return Math.max(0, Math.floor((now - t) / DAY));
}

/** Day thresholds for the 1–4 dots next to "days in column". */
const DOT_STEPS = [1, 3, 7, 14];

/** ageDots is how many of the four dots are filled after `days` in a column. */
export function ageDots(days: number): number {
  return DOT_STEPS.filter((d) => days >= d).length;
}

export function daysLabel(days: number): string {
  return days < 1 ? "<1d" : `${days}d`;
}

/** inStatus phrases being in a status: "In Code review", but "In Progress" rather than "In In Progress". */
export function inStatus(statusName: string): string {
  return /^in\s/i.test(statusName) ? statusName : `In ${statusName}`;
}

export function daysTitle(statusName: string, days: number): string {
  if (days < 1) return `${inStatus(statusName)} for less than a day`;
  return `${inStatus(statusName)} for ${days} day${days === 1 ? "" : "s"}`;
}

/** localDate is a Date's calendar day (YYYY-MM-DD) in the viewer's time zone. */
export function localDate(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/**
 * dueLabel formats a due date (YYYY-MM-DD) for a card: "Oct 30", with the year when it is not
 * this year. It is overdue when the day has passed and the issue isn't done.
 */
export function dueLabel(due: string, now: Date, done: boolean, locale?: string): { text: string; overdue: boolean; title: string } {
  const [y, m, d] = due.split("-").map(Number);
  const date = new Date(y, m - 1, d);
  const sameYear = y === now.getFullYear();
  const text = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", ...(sameYear ? {} : { year: "numeric" }) }).format(date);
  const full = new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
  const overdue = !done && due < localDate(now);
  return { text, overdue, title: overdue ? `Overdue: due ${full}` : `Due ${full}` };
}
