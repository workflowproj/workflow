import { useState } from "react";
import { ChevronRight, Flag as FlagIcon, Link2 } from "lucide-react";
import { Badge, Select, type Tone } from "@/components/ui";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { ProjectAvatar } from "@/layout/AppShell";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";
import { donePercent, flagLabel, forecastLate, groupByTeam, progressText } from "./model";
import { useSetInitiative, type PlanningFlag, type ProgrammeEpic, type ProgrammeInitiative } from "./queries";
import type { Schemas } from "@/api/client";

type Issue = Schemas["Issue"];
type ProjectRef = Schemas["ProjectRef"];

const flagTone: Record<PlanningFlag["kind"], Tone> = { late: "warning", overdue: "danger", unscheduled: "neutral", blocked: "danger" };

/** Flags lists an item's planning flags as badges; each badge's title says why. */
export function Flags({ flags }: { flags: PlanningFlag[] }) {
  if (flags.length === 0) return null;
  return (
    <ul className="flex flex-wrap gap-1" aria-label="Flags">
      {flags.map((f) => (
        <li key={f.kind} title={f.message}>
          <Badge tone={flagTone[f.kind]}>
            <span className="sr-only">{f.message}</span>
            <span aria-hidden>{flagLabel[f.kind]}</span>
          </Badge>
        </li>
      ))}
    </ul>
  );
}

/** ProgressBar is done, in progress and to do as one bar, with "3 of 8 done · 5 of 21 pts". */
export function ProgressBar({ progress, className }: { progress: Schemas["IssueProgress"]; className?: string }) {
  const { total, done, inProgress } = progress;
  const pct = (n: number) => (total === 0 ? 0 : (n / total) * 100);
  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <div
        role="progressbar"
        aria-label="Issues done"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={donePercent(progress)}
        aria-valuetext={progressText(progress)}
        className="flex h-1.5 w-full overflow-hidden rounded-full bg-surface-3"
      >
        <span className="h-full bg-done" style={{ width: `${pct(done)}%` }} />
        <span className="h-full bg-progress" style={{ width: `${pct(inProgress)}%` }} />
      </div>
      <span className="text-xs whitespace-nowrap text-muted tabular-nums">{progressText(progress)}</span>
    </div>
  );
}

/** Dates is "Due Dec 18 · Forecast Dec 22", the forecast in red when it is after the due date. */
function Dates({ due, forecast }: { due?: string; forecast?: string }) {
  const late = forecastLate(due, forecast);
  return (
    <span className="text-xs whitespace-nowrap text-muted">
      {due ? `Due ${formatDate(due)}` : "No due date"}
      {forecast && (
        <>
          {" · "}
          <span className={cn(late && "font-medium text-danger")}>Forecast {formatDate(forecast)}</span>
        </>
      )}
    </span>
  );
}

function KeyButton({ issue, onOpen }: { issue: Pick<Issue, "key" | "type">; onOpen: (key: string) => void }) {
  return (
    <button type="button" onClick={() => onOpen(issue.key)} className="inline-flex shrink-0 items-center gap-1.5 font-mono text-xs text-muted hover:text-accent hover:underline">
      <IssueTypeIcon type={issue.type} />
      {issue.key}
    </button>
  );
}

/**
 * InitiativeCard is one initiative: its status, dates, the work under it and its flags, expanding
 * to the epics under it, team by team.
 */
export function InitiativeCard({ item, teams, onOpen }: { item: ProgrammeInitiative; teams: ProjectRef[]; onOpen: (key: string) => void }) {
  const [open, setOpen] = useState(true);
  const { issue } = item;
  const groups = groupByTeam(item.epics);
  const panel = `initiative-${issue.key}`;
  return (
    <section aria-labelledby={`${panel}-title`} className="rounded-lg border border-line bg-surface shadow-sm" data-initiative={issue.key}>
      <div className="flex flex-col gap-3 p-4 md:flex-row md:items-center">
        <div className="flex min-w-0 flex-1 items-start gap-2">
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            aria-controls={panel}
            aria-label={open ? `Hide the epics of ${issue.key}` : `Show the epics of ${issue.key}`}
            className="mt-0.5 rounded p-0.5 text-muted hover:bg-surface-2 hover:text-ink"
          >
            <ChevronRight className={cn("size-4 transition-transform", open && "rotate-90")} />
          </button>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <KeyButton issue={issue} onOpen={onOpen} />
              <StatusBadge status={issue.status} />
            </div>
            <h3 id={`${panel}-title`} className="mt-1 text-[15px] leading-snug font-semibold">
              <button type="button" onClick={() => onOpen(issue.key)} className="text-left hover:underline">
                {issue.summary}
              </button>
            </h3>
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
              <Dates due={issue.dueDate} forecast={item.forecastDate} />
              <span className="text-xs text-muted">
                {item.epics.length === 1 ? "1 epic" : `${item.epics.length} epics`}
                {groups.length > 0 && ` in ${groups.length === 1 ? "1 team" : `${groups.length} teams`}`}
              </span>
            </div>
          </div>
        </div>
        <div className="flex flex-col gap-2 md:w-72 md:shrink-0">
          <ProgressBar progress={item.progress} />
          <Flags flags={item.flags} />
        </div>
      </div>
      {open && (
        <div id={panel} className="border-t border-line">
          {groups.length === 0 ? (
            <p className="px-4 py-3 text-[13px] text-muted">
              No epics yet. Give a team's epic this initiative from the epic's <span className="font-medium text-ink">Initiative</span> field, or link one from the list of
              epics under no initiative below.
            </p>
          ) : (
            groups.map((g) => (
              <div key={g.projectKey} className="border-b border-line last:border-b-0">
                <h4 className="flex items-center gap-2 bg-surface-2/60 px-4 py-1.5 text-xs font-semibold text-muted">
                  <ProjectAvatar projectKey={g.projectKey} size={16} />
                  {teams.find((t) => t.key === g.projectKey)?.name ?? g.projectKey}
                </h4>
                <ul className="divide-y divide-line">
                  {g.epics.map((e) => (
                    <EpicRow key={e.issue.key} epic={e} onOpen={onOpen} />
                  ))}
                </ul>
              </div>
            ))
          )}
        </div>
      )}
    </section>
  );
}

function EpicRow({ epic, onOpen }: { epic: ProgrammeEpic; onOpen: (key: string) => void }) {
  const { issue } = epic;
  return (
    <li className="flex flex-col gap-2 px-4 py-2.5 md:flex-row md:items-center md:gap-4" data-epic={issue.key}>
      <div className="flex min-w-0 flex-1 items-center gap-2 md:pl-6">
        <KeyButton issue={issue} onOpen={onOpen} />
        <button type="button" onClick={() => onOpen(issue.key)} className="min-w-0 truncate text-left text-sm hover:underline">
          {issue.summary}
        </button>
        <StatusBadge status={issue.status} className="shrink-0" />
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 md:grid md:shrink-0 md:grid-cols-[16.5rem_13rem_6.5rem] md:items-center">
        <Dates due={issue.dueDate} forecast={epic.forecastDate} />
        <ProgressBar progress={epic.progress} className="w-52 md:w-auto" />
        <Flags flags={epic.flags} />
      </div>
    </li>
  );
}

/**
 * UnlinkedEpics lists open team epics that belong to no initiative, each with a picker to link it
 * to one (for people who can edit the epic).
 */
export function UnlinkedEpics({
  epics,
  total,
  initiatives,
  canEdit,
  onOpen,
}: {
  epics: Issue[];
  total: number;
  initiatives: ProgrammeInitiative[];
  canEdit: (projectKey: string) => boolean;
  onOpen: (key: string) => void;
}) {
  const setInitiative = useSetInitiative();
  const open = initiatives.filter((i) => i.issue.status.category !== "done");
  if (total === 0) return null;
  return (
    <section aria-labelledby="unlinked-title" className="rounded-lg border border-line bg-surface shadow-sm">
      <div className="flex items-start gap-2 border-b border-line px-4 py-3">
        <FlagIcon className="mt-0.5 size-4 shrink-0 text-muted" aria-hidden />
        <div>
          <h2 id="unlinked-title" className="text-sm font-semibold">
            Team epics under no initiative ({total})
          </h2>
          <p className="text-[13px] text-muted">Open epics in the programme's teams that don't yet say which initiative they serve.</p>
        </div>
      </div>
      <ul className="divide-y divide-line">
        {epics.map((e) => (
          <li key={e.key} className="flex flex-col gap-2 px-4 py-2.5 sm:flex-row sm:items-center sm:gap-3" data-unlinked={e.key}>
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <ProjectAvatar projectKey={e.projectKey} size={16} />
              <KeyButton issue={e} onOpen={onOpen} />
              <button type="button" onClick={() => onOpen(e.key)} className="min-w-0 truncate text-left text-sm hover:underline">
                {e.summary}
              </button>
            </div>
            {canEdit(e.projectKey) && open.length > 0 && (
              <label className="flex items-center gap-2 text-[13px] text-muted sm:w-72">
                <Link2 className="size-3.5 shrink-0" aria-hidden />
                <span className="sr-only">Initiative for {e.key}</span>
                <Select
                  value=""
                  disabled={setInitiative.isPending}
                  onChange={(ev) => ev.target.value && setInitiative.mutate({ epicKey: e.key, initiativeKey: ev.target.value })}
                  className="h-8 text-[13px]"
                >
                  <option value="">Link to an initiative…</option>
                  {open.map((i) => (
                    <option key={i.issue.key} value={i.issue.key}>
                      {i.issue.key} {i.issue.summary}
                    </option>
                  ))}
                </Select>
              </label>
            )}
          </li>
        ))}
      </ul>
      {total > epics.length && <p className="border-t border-line px-4 py-2 text-xs text-muted">Showing {epics.length} of {total}.</p>}
    </section>
  );
}
