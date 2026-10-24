import { useMemo, useState } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, Timer, TrendingDown } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Card, EmptyState, PageHeader, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { sprintsQuery, type Sprint } from "@/features/board/queries";
import { formatPoints } from "@/features/board/model";
import { formatDay, formatRange, remainingLabel } from "@/features/backlog/dates";
import { velocityQuery } from "@/features/dashboards/api";
import { BurndownChart, BurnupChart, measureOf, sprintMeasure, type Measure } from "@/features/reports/BurndownChart";
import { Segmented, StatTile } from "@/features/reports/ChartBits";
import { formatDuration } from "@/features/reports/chart-utils";
import { CycleTimeChart } from "@/features/reports/CycleTimeChart";
import { VelocityChart } from "@/features/reports/VelocityChart";

const route = getRouteApi("/app/projects/$projectKey/reports");

const reportQuery = (sprintId: string) => ({
  queryFn: () => unwrap(api.GET("/sprints/{sprintId}/report", { params: { path: { sprintId } } })),
});

export function ReportsPage() {
  const { projectKey } = route.useParams();
  const { data: sprints, isLoading, error } = useQuery(sprintsQuery(projectKey));
  const started = (sprints ?? []).filter((s) => s.state !== "future");
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6 p-4 md:p-6">
      <PageHeader title="Reports" subtitle="Sprint progress, velocity and how long work takes to get done." />
      {error && <ErrorBox error={error} />}
      {isLoading ? (
        <PageSpinner label="Loading reports" />
      ) : started.length > 0 ? (
        <>
          <SprintReport projectKey={projectKey} sprints={started} />
          <Velocity projectKey={projectKey} hasClosed={started.some((s) => s.state === "closed")} />
        </>
      ) : (
        <Card title="Sprint report">
          <EmptyState icon={<TrendingDown className="size-8" />} title="No sprints yet" body="Burndown and velocity appear once a sprint has started. Projects without sprints can still use cycle time below." />
        </Card>
      )}
      <CycleTime projectKey={projectKey} />
    </div>
  );
}

function SprintReport({ projectKey, sprints }: { projectKey: string; sprints: Sprint[] }) {
  // Newest first: the active sprint, then the most recently completed.
  const ordered = useMemo(() => [...sprints].sort((a, b) => (a.state === "active" ? -1 : b.state === "active" ? 1 : (b.completedAt ?? "").localeCompare(a.completedAt ?? ""))), [sprints]);
  const [picked, setPicked] = useState<string>();
  const sprint = ordered.find((s) => s.id === picked) ?? ordered[0];
  const { data, isLoading, error } = useQuery({ queryKey: ["sprints", projectKey, "report", sprint.id], ...reportQuery(sprint.id) });
  const [view, setView] = useState<"burndown" | "burnup">("burndown");
  const [pickedMeasure, setMeasure] = useState<Measure>();
  const left = sprint.state === "active" ? remainingLabel(sprint.endDate) : null;
  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <TrendingDown className="size-4 text-muted" /> Sprint report
        </span>
      }
      actions={
        <div className="w-56">
          <Select aria-label="Sprint" value={sprint.id} onChange={(e) => setPicked(e.target.value)} className="h-8 text-[13px]">
            {ordered.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
                {s.state === "active" ? " (active)" : ""}
              </option>
            ))}
          </Select>
        </div>
      }
    >
      <div className="p-4">
        <p className="mb-4 text-sm text-muted">
          {formatRange(sprint.startDate, sprint.endDate)}
          {left && ` · ${left.text}`}
          {sprint.completedAt && ` · completed ${formatDay(sprint.completedAt)}`}
          {sprint.goal && <span className="mt-0.5 block text-ink">{sprint.goal}</span>}
        </p>
        {error && <ErrorBox error={error} />}
        {isLoading || !data ? (
          <PageSpinner label="Loading sprint report" />
        ) : (
          <SprintNumbers report={data} measure={sprintMeasure(data, pickedMeasure)} left={left?.text} view={view} onView={setView} onMeasure={setMeasure} />
        )}
      </div>
    </Card>
  );
}

/** SprintNumbers is the sprint's totals, its scope change and the burndown or burnup. */
function SprintNumbers({
  report,
  measure,
  left,
  view,
  onView,
  onMeasure,
}: {
  report: Schemas["SprintReport"];
  measure: Measure;
  left?: string;
  view: "burndown" | "burnup";
  onView: (v: "burndown" | "burnup") => void;
  onMeasure: (m: Measure) => void;
}) {
  const m = measureOf(report, measure);
  const last = report.burndown[report.burndown.length - 1];
  const scope = last ? m.scope(last) : m.committed + m.added - m.removed;
  const remaining = last ? m.remaining(last) : Math.max(0, scope - m.completed);
  const pct = scope > 0 ? Math.round((m.completed / scope) * 100) : null;
  const n = (v: number, word: string) => `${v} ${word}${v === 1 ? "" : "s"}`;
  const issuesOf = (pts: number, issues: number) => (measure === "points" ? ` (${n(issues, "issue")})` : pts > 0 ? ` (${formatPoints(pts)} pts)` : "");
  const change =
    m.added === 0 && m.removed === 0
      ? "No scope change since the sprint started."
      : [m.added > 0 && `+${m.format(m.added)} ${m.unit} added${issuesOf(report.addedPoints, report.addedIssues)}`, m.removed > 0 && `−${m.format(m.removed)} ${m.unit} removed${issuesOf(report.removedPoints, report.removedIssues)}`]
          .filter(Boolean)
          .join(", ") + " after the sprint started.";
  return (
    <>
      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Committed" value={`${m.format(m.committed)} ${m.unit}`} sub={measure === "points" ? `${n(report.committedIssues, "issue")} at sprint start` : "at sprint start"} />
        <StatTile label="Completed" value={`${m.format(m.completed)} ${m.unit}`} sub={pct == null ? undefined : `${pct}% of the current scope`} />
        <StatTile label="Scope change" value={m.added === 0 && m.removed === 0 ? "None" : `${m.added - m.removed >= 0 ? "+" : "−"}${m.format(Math.abs(m.added - m.removed))} ${m.unit}`} sub={`now ${m.format(scope)} ${m.unit} in scope`} />
        <StatTile label="Remaining" value={`${m.format(remaining)} ${m.unit}`} sub={left ?? (report.sprint.state === "closed" ? "Sprint completed" : undefined)} />
      </div>
      <p className="mb-4 text-[13px] text-muted">{change}</p>
      {report.burndown.length > 0 ? (
        <>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Segmented
              label="Chart"
              value={view}
              onChange={onView}
              options={[
                { value: "burndown", label: "Burndown" },
                { value: "burnup", label: "Burnup" },
              ]}
            />
            <Segmented
              label="Measure"
              value={measure}
              onChange={onMeasure}
              options={[
                { value: "points", label: "Story points" },
                { value: "issues", label: "Issues" },
              ]}
            />
          </div>
          {view === "burndown" ? <BurndownChart report={report} measure={measure} /> : <BurnupChart report={report} measure={measure} />}
        </>
      ) : (
        <p className="text-sm text-muted">There's no burndown for this sprint yet.</p>
      )}
    </>
  );
}

function Velocity({ projectKey, hasClosed }: { projectKey: string; hasClosed: boolean }) {
  const { data, isLoading, error } = useQuery({ ...velocityQuery(projectKey, 8), enabled: hasClosed });
  const points = !data || data.sprints.some((s) => s.committedPoints > 0 || s.completedPoints > 0);
  const average = points ? data?.averagePoints : data?.averageIssues;
  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <BarChart3 className="size-4 text-muted" /> Velocity
        </span>
      }
      actions={!!data?.sprints.length && <span className="text-xs text-muted">Average {formatPoints(Math.round((average ?? 0) * 10) / 10)} {points ? "points" : "issues"} per sprint</span>}
    >
      <div className="p-4">
        {error ? (
          <ErrorBox error={error} />
        ) : !hasClosed || data?.sprints.length === 0 ? (
          <EmptyState title="No completed sprints yet" body="Velocity compares committed and completed story points once sprints are completed." />
        ) : isLoading || !data ? (
          <PageSpinner label="Loading velocity" />
        ) : (
          <VelocityChart
            unit={points ? "points" : "issues"}
            average={average}
            data={data.sprints.map((s) => ({ id: s.sprint.id, name: s.sprint.name, committed: s.committedPoints, completed: s.completedPoints, committedIssues: s.committedIssues, completedIssues: s.completedIssues }))}
          />
        )}
      </div>
    </Card>
  );
}

const WINDOWS = [14, 30, 90, 180];

function CycleTime({ projectKey }: { projectKey: string }) {
  const navigate = useNavigate();
  const [days, setDays] = useState(30);
  const { data, isLoading, error, isPlaceholderData } = useQuery({
    queryKey: ["cycle-time", projectKey, days],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/reports/cycle-time", { params: { path: { projectKey }, query: { days } } })),
    placeholderData: (prev) => prev,
  });
  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <Timer className="size-4 text-muted" /> Cycle time
        </span>
      }
      actions={
        <div className="w-40">
          <Select aria-label="Time window" value={days} onChange={(e) => setDays(Number(e.target.value))} className="h-8 text-[13px]">
            {WINDOWS.map((d) => (
              <option key={d} value={d}>
                Last {d} days
              </option>
            ))}
          </Select>
        </div>
      }
    >
      <div className={`p-4 transition-opacity ${isPlaceholderData ? "opacity-60" : ""}`}>
        <p className="mb-4 text-sm text-muted">Time from when work started on an issue to when it was resolved.</p>
        {error && <ErrorBox error={error} />}
        {isLoading || !data ? (
          !error && <PageSpinner label="Loading cycle time" />
        ) : data.issues.length === 0 ? (
          <EmptyState title={`Nothing resolved in the last ${days} days`} body="Cycle time appears as issues are resolved." />
        ) : (
          <>
            <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
              <StatTile label="Median" value={formatDuration(data.medianHours)} sub="half of issues are faster" />
              <StatTile label="85th percentile" value={formatDuration(data.p85Hours)} sub="most issues finish within" />
              <StatTile label="Average" value={formatDuration(data.averageHours)} />
              <StatTile label="Resolved" value={data.issues.length} sub={`in the last ${days} days`} />
            </div>
            <CycleTimeChart report={data} onOpen={(key) => navigate({ to: "/browse/$issueKey", params: { issueKey: key } })} />
          </>
        )}
      </div>
    </Card>
  );
}
