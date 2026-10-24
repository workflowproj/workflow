import type { Schemas } from "@/api/client";
import { cn } from "@/lib/cn";
import { formatDay, formatSpan, toDay } from "./model";

type Sprint = Schemas["Sprint"];
type Release = Schemas["Release"];

const dated = (sprints: Sprint[]) => sprints.filter((s) => s.startDate && s.endDate);
const shown = (releases: Release[]) => releases.filter((r) => r.releaseDate && r.state !== "archived");

function releaseTone(r: Release, today: number) {
  if (r.state === "released") return { fill: "bg-success border-success", label: "Released" };
  if (toDay(r.releaseDate!) < today) return { fill: "bg-warning border-warning", label: "Overdue" };
  return { fill: "bg-surface border-accent", label: "Upcoming" };
}

/** SprintBands shades each sprint's days across a project's rows; the active sprint stands out. */
export function SprintBands({ sprints, x, ppd, top, height }: { sprints: Sprint[]; x: (day: number) => number; ppd: number; top: number; height: number }) {
  return (
    <>
      {dated(sprints).map((s) => {
        const a = toDay(s.startDate!);
        const b = toDay(s.endDate!);
        const active = s.state === "active";
        return (
          <div
            key={s.id}
            aria-hidden
            className={cn("absolute border-x", active ? "border-accent/25 bg-accent-soft/60" : "border-line bg-surface-2/60")}
            style={{ left: x(a), width: (b - a + 1) * ppd, top, height }}
          />
        );
      })}
    </>
  );
}

/** ReleaseLines draws a faint vertical line on each release date across a project's rows. */
export function ReleaseLines({ releases, x, ppd, top, height }: { releases: Release[]; x: (day: number) => number; ppd: number; top: number; height: number }) {
  return (
    <>
      {shown(releases).map((r) => (
        <div
          key={r.id}
          aria-hidden
          className={cn("absolute w-px", r.state === "released" ? "bg-success/40" : "bg-accent/40")}
          style={{ left: x(toDay(r.releaseDate!)) + ppd / 2, top, height }}
        />
      ))}
    </>
  );
}

/**
 * MarkerLane labels a project's sprints (named chips over their span) and releases (a diamond
 * and name on the release date). It sits in the project's row, or in the header for one project.
 */
export function MarkerLane({ sprints, releases, x, ppd, today, height }: { sprints: Sprint[]; releases: Release[]; x: (day: number) => number; ppd: number; today: number; height: number }) {
  return (
    <>
      {dated(sprints).map((s) => {
        const a = toDay(s.startDate!);
        const b = toDay(s.endDate!);
        const active = s.state === "active";
        return (
          <span
            key={s.id}
            title={`${s.name} (${s.state === "closed" ? "completed" : s.state}) · ${formatSpan(a, b)}${s.goal ? `\n${s.goal}` : ""}`}
            className={cn(
              "absolute truncate rounded px-1.5 text-[10px] leading-4 font-semibold",
              active ? "bg-accent-soft text-accent" : "bg-surface-3 text-muted",
            )}
            style={{ left: x(a) + 1, maxWidth: Math.max(0, (b - a + 1) * ppd - 2), top: height / 2 - 8 }}
          >
            {s.name}
          </span>
        );
      })}
      {shown(releases).map((r) => {
        const d = toDay(r.releaseDate!);
        const tone = releaseTone(r, today);
        return (
          <span
            key={r.id}
            title={`Release ${r.name} · ${formatDay(d)} · ${tone.label} · ${r.doneCount}/${r.issueCount} issues done`}
            className="absolute z-[1] flex items-center gap-1 rounded bg-surface/85 pr-1 text-[10px] leading-4 font-semibold text-ink"
            style={{ left: x(d) + ppd / 2 - 5, top: height / 2 - 8 }}
          >
            <span aria-hidden className={cn("size-2.5 rotate-45 rounded-[2px] border-2", tone.fill)} />
            <span className="sr-only">Release </span>
            {r.name}
          </span>
        );
      })}
    </>
  );
}
