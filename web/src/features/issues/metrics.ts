import type { Schemas } from "@/api/client";
import type { Status } from "@/api/types";

/* Formatting and the time-in-status bar behind the issue view's Metrics panel. */

export type IssueMetrics = Schemas["IssueMetrics"];

/** formatHours renders a duration compactly: "<1m", "45m", "5h 30m", "3d 4h", "12d". */
export function formatHours(hours: number): string {
  const mins = Math.round(Math.max(0, hours) * 60);
  if (mins < 1) return "<1m";
  if (mins < 60) return `${mins}m`;
  if (mins < 24 * 60) {
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    return m ? `${h}h ${m}m` : `${h}h`;
  }
  const total = Math.round(mins / 60);
  const d = Math.floor(total / 24);
  const h = total % 24;
  return d < 7 && h ? `${d}d ${h}h` : `${d}d`;
}

export type Segment = {
  status: Status;
  /** Time counted in the bar. */
  hours: number;
  /** Total time in the status (what the list shows). */
  total: number;
  visits: number;
  current: boolean;
  /** Share of the bar, 0–1. */
  share: number;
  /** Textured, to tell it from a neighbour of the same category. */
  alt: boolean;
};

/**
 * statusSegments lays out the time-in-status bar. Statuses keep the order they were first
 * visited in. Once an issue is resolved the bar stops there: the current visit to a done
 * status isn't work, so it is left out of the bar (the list still shows the total).
 */
export function statusSegments(m: IssueMetrics, currentStatusId: string): { segments: Segment[]; total: number; resolved: boolean } {
  let resolved = false;
  const rows = m.timeInStatus.map((t) => {
    const current = t.status.id === currentStatusId;
    let hours = Math.max(0, t.hours);
    if (current && t.status.category === "done") {
      resolved = true;
      hours = Math.max(0, hours - m.statusAgeHours);
    }
    return { status: t.status, hours, total: t.hours, visits: t.visits, current };
  });
  const total = rows.reduce((n, r) => n + r.hours, 0);
  let prev: Status | undefined;
  let alt = false;
  const segments = rows.map((r) => {
    const shown = r.hours > 0;
    if (shown) {
      alt = prev?.category === r.status.category ? !alt : false;
      prev = r.status;
    }
    return { ...r, share: total > 0 ? r.hours / total : 0, alt: shown && alt };
  });
  return { segments, total, resolved };
}
