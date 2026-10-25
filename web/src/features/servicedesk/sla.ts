import type { Sla } from "./api";

/**
 * formatSeconds writes a duration compactly: "45m", "3h 20m", "52h". SLA times are working time
 * (e.g. 40 hours is a business week), so it never counts in days.
 */
export function formatSeconds(total: number): string {
  const s = Math.abs(Math.round(total));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  return `${m}m`;
}

export type SlaTone = "success" | "warning" | "danger" | "neutral";

/**
 * slaStatus summarises a clock for a badge: met or missed once stopped; otherwise the time left
 * (or over), with "at risk" under a quarter of the goal (and at most an hour).
 */
export function slaStatus(s: Sla): { tone: SlaTone; label: string; detail: string } {
  const goal = formatSeconds(s.targetSeconds);
  if (s.state === "stopped") {
    return s.breached
      ? { tone: "danger", label: "Missed", detail: `${s.name}: missed the ${goal} goal by ${formatSeconds(s.elapsedSeconds - s.targetSeconds)}` }
      : { tone: "success", label: "Met", detail: `${s.name}: met the ${goal} goal in ${formatSeconds(s.elapsedSeconds)}` };
  }
  const paused = s.state === "paused" ? " (paused)" : "";
  if (s.breached || s.remainingSeconds < 0) {
    return {
      tone: "danger",
      label: `-${formatSeconds(s.remainingSeconds)}${paused}`,
      detail: `${s.name}: breached ${formatSeconds(s.remainingSeconds)} ago (goal ${goal})${paused}`,
    };
  }
  const atRisk = s.remainingSeconds <= Math.min(s.targetSeconds / 4, 3600);
  return {
    tone: s.state === "paused" ? "neutral" : atRisk ? "warning" : "success",
    label: `${formatSeconds(s.remainingSeconds)}${paused}`,
    detail: `${s.name}: ${formatSeconds(s.remainingSeconds)} left of ${goal}${paused}`,
  };
}
