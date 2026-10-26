import { useQuery } from "@tanstack/react-query";
import { CalendarClock, FolderKanban, TrendingDown } from "lucide-react";
import type { Schemas } from "@/api/client";
import { ErrorBox } from "@/components/ErrorBox";
import { formatDay, formatRange, remainingLabel } from "@/features/backlog/dates";
import { sprintsQuery } from "@/features/board/queries";
import { BurndownChart, BurnupChart, measureOf, sprintMeasure } from "@/features/reports/BurndownChart";
import { CategoryBar } from "@/features/reports/BreakdownChart";
import { VelocityChart } from "@/features/reports/VelocityChart";
import { formatPoints } from "@/features/board/model";
import { cn } from "@/lib/cn";
import { breakdownQuery, sprintReportQuery, velocityQuery } from "./api";
import { GadgetBody, GadgetFrame, GadgetLoading, GadgetMessage, ReportLink, useGadget } from "./GadgetFrame";
import { gadgetTitle, pickSprint, scopeSummary } from "./model";

type Sprint = Schemas["Sprint"];
type SprintReport = Schemas["SprintReport"];

const CHART_H = 220;

function NoProject() {
  return <GadgetMessage icon={<FolderKanban className="size-6" />} title="Choose a project" body="Pick the project whose sprints this gadget follows in its settings." />;
}

/** sprintLine is a sprint's dates and how long it has left (or when it finished). */
function sprintLine(s: Sprint): string {
  const left = s.state === "active" ? remainingLabel(s.endDate)?.text : s.completedAt ? `completed ${formatDay(s.completedAt)}` : "";
  return [formatRange(s.startDate, s.endDate), left].filter(Boolean).join(" · ");
}

/**
 * useGadgetSprint finds the sprint a gadget shows (chosen, else active, else last completed)
 * and loads its report.
 */
function useGadgetSprint() {
  const { gadget } = useGadget();
  const projectKey = gadget.config.projectKey ?? "";
  const sprints = useQuery({ ...sprintsQuery(projectKey), enabled: !!projectKey, refetchInterval: 60_000 });
  const sprint = sprints.data ? pickSprint(sprints.data, gadget.config.sprintId) : undefined;
  const report = useQuery({ ...sprintReportQuery(projectKey, sprint?.id ?? ""), enabled: !!sprint });
  return { gadget, projectKey, sprints, sprint, report };
}

/** SprintFrame wraps the sprint gadgets: it resolves the sprint and covers the shared states. */
function SprintFrame({ children }: { children: (report: SprintReport, sprint: Sprint) => React.ReactNode }) {
  const { gadget, projectKey, sprints, sprint, report } = useGadgetSprint();
  const title = gadgetTitle(gadget, sprint?.name);
  let body: React.ReactNode;
  if (!projectKey) body = <NoProject />;
  else if (sprints.error) body = <ErrorBox error={sprints.error} />;
  else if (!sprints.data) body = <GadgetLoading />;
  else if (!sprint)
    body = gadget.config.sprintId ? (
      <GadgetMessage icon={<CalendarClock className="size-6" />} title="That sprint no longer exists" body="Choose another sprint, or follow the active one, in this gadget's settings." />
    ) : (
      <GadgetMessage icon={<CalendarClock className="size-6" />} title={`No active or completed sprint in ${projectKey}`} body="This fills in once a sprint starts." />
    );
  else body = <GadgetBody query={report}>{(r) => children(r, sprint)}</GadgetBody>;
  return (
    <GadgetFrame title={title} subtitle={sprint ? sprintLine(sprint) : scopeSummary(gadget)} link={projectKey ? <ReportLink projectKey={projectKey} /> : undefined} dimmed={report.isPlaceholderData}>
      {body}
    </GadgetFrame>
  );
}

export function SprintChartGadget({ kind }: { kind: "burndown" | "burnup" }) {
  const { gadget } = useGadget();
  return (
    <SprintFrame>
      {(r) =>
        r.burndown.length === 0 ? (
          <GadgetMessage icon={<TrendingDown className="size-6" />} title="No days to plot yet" body="The chart starts on the sprint's first day." />
        ) : kind === "burndown" ? (
          <BurndownChart report={r} measure={gadget.config.measure} height={CHART_H} />
        ) : (
          <BurnupChart report={r} measure={gadget.config.measure} height={CHART_H} />
        )
      }
    </SprintFrame>
  );
}

export function SprintHealthGadget() {
  return <SprintFrame>{(r, sprint) => <SprintHealth report={r} sprint={sprint} />}</SprintFrame>;
}

function SprintHealth({ report, sprint }: { report: SprintReport; sprint: Sprint }) {
  const { gadget } = useGadget();
  const measure = sprintMeasure(report, gadget.config.measure);
  const m = measureOf(report, measure);
  const last = report.burndown[report.burndown.length - 1];
  const scope = last ? m.scope(last) : m.committed + m.added - m.removed;
  const done = last ? m.done(last) : m.completed;
  const pct = scope > 0 ? Math.round((done / scope) * 100) : null;
  const left = sprint.state === "active" ? remainingLabel(sprint.endDate) : null;
  const byStatus = useQuery(breakdownQuery({ projectKeys: [gadget.config.projectKey!], jql: `sprint = "${sprint.id}" AND level >= 0` }, "statusCategory"));
  // The status bar counts issues when the sprint is measured in hours (estimates are per issue, not per status).
  const values = Object.fromEntries((byStatus.data?.groups ?? []).map((g) => [g.key, measure === "points" ? g.points : g.count]));
  const unit = m.unit;
  const barUnit = measure === "hours" ? "issues" : unit;
  const barFormat = measure === "hours" ? (n: number) => String(Math.round(n)) : m.format;
  const facts: [string, string, string?][] = [
    ["Committed", `${m.format(m.committed)} ${unit}`],
    [m.doneLabel, `${m.format(m.completed)} ${unit}`],
    ["Added", m.added > 0 ? `+${m.format(m.added)} ${unit}` : "None", m.added > 0 ? "text-warning" : undefined],
    ["Removed", m.removed > 0 ? `−${m.format(m.removed)} ${unit}` : "None"],
  ];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-end justify-between gap-3">
        <div>
          <p className="text-4xl leading-none font-semibold tracking-tight">{pct === null ? "–" : `${pct}%`}</p>
          <p className="mt-1.5 text-xs text-muted">
            {measure === "hours" ? "logged" : "done"} · {m.format(done)} of {m.format(scope)} {unit}
          </p>
        </div>
        <div className="text-right">
          {left ? (
            <>
              <p className={cn("text-lg leading-none font-semibold", left.overdue && "text-danger")}>{left.text.replace(/ (left|overdue)$/, "")}</p>
              <p className="mt-1.5 text-xs text-muted">{left.overdue ? "overdue" : left.text.endsWith("left") ? "left" : "\u00a0"}</p>
            </>
          ) : (
            <p className="text-xs text-muted">{sprint.state === "closed" ? `Completed ${sprint.completedAt ? formatDay(sprint.completedAt) : ""}` : ""}</p>
          )}
        </div>
      </div>
      {byStatus.error ? <ErrorBox error={byStatus.error} /> : <CategoryBar values={values} unit={barUnit} format={barFormat} />}
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 border-t border-line pt-3 text-[13px]">
        {facts.map(([k, v, cls]) => (
          <div key={k} className="flex items-baseline justify-between gap-2">
            <dt className="text-muted">{k}</dt>
            <dd className={cn("font-semibold tabular-nums", cls)}>{v}</dd>
          </div>
        ))}
      </dl>
      {sprint.goal && <p className="line-clamp-2 text-[13px] text-muted">Goal: {sprint.goal}</p>}
    </div>
  );
}

export function VelocityGadget() {
  const { gadget } = useGadget();
  const projectKey = gadget.config.projectKey ?? "";
  const count = gadget.config.count ?? 7;
  const q = useQuery({ ...velocityQuery(projectKey, count), enabled: !!projectKey });
  // Velocity counts points or issues; hours (a sprint gadget measure) falls back to the default.
  const chosen = gadget.config.measure === "hours" ? undefined : gadget.config.measure;
  const measure = chosen ?? (q.data?.sprints.some((s) => s.committedPoints > 0 || s.completedPoints > 0) ? "points" : "issues");
  const average = measure === "points" ? q.data?.averagePoints : q.data?.averageIssues;
  const subtitle = q.data?.sprints.length
    ? `Average ${formatPoints(Math.round((average ?? 0) * 10) / 10)} ${measure === "points" ? "pts" : "issues"} per sprint · last ${q.data.sprints.length} sprints`
    : scopeSummary(gadget);
  return (
    <GadgetFrame title={gadgetTitle(gadget)} subtitle={subtitle} link={projectKey ? <ReportLink projectKey={projectKey} /> : undefined} dimmed={q.isPlaceholderData}>
      {!projectKey ? (
        <NoProject />
      ) : (
        <GadgetBody
          query={q}
          empty={(d) => d.sprints.length === 0 && <GadgetMessage icon={<CalendarClock className="size-6" />} title={`No completed sprints in ${projectKey} yet`} body="Velocity compares committed and completed work once sprints are completed." />}
        >
          {(d) => (
            <VelocityChart
              unit={measure}
              average={average}
              height={CHART_H}
              data={d.sprints.map((s) => ({ id: s.sprint.id, name: s.sprint.name, committed: s.committedPoints, completed: s.completedPoints, committedIssues: s.committedIssues, completedIssues: s.completedIssues }))}
            />
          )}
        </GadgetBody>
      )}
    </GadgetFrame>
  );
}
