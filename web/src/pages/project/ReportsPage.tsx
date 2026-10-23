import { useMemo, useState } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useQueries, useQuery } from "@tanstack/react-query";
import { BarChart3, Timer, TrendingDown } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Card, EmptyState, PageHeader, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { sprintsQuery, type Sprint } from "@/features/board/queries";
import { formatPoints } from "@/features/board/model";
import { formatDay, formatRange, remainingLabel } from "@/features/backlog/dates";
import { BurndownChart } from "@/features/reports/BurndownChart";
import { StatTile } from "@/features/reports/ChartBits";
import { formatDuration } from "@/features/reports/chart-utils";
import { CycleTimeChart } from "@/features/reports/CycleTimeChart";
import { VelocityChart, type VelocityPoint } from "@/features/reports/VelocityChart";

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
          <Velocity sprints={started.filter((s) => s.state === "closed")} />
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
  const pct = data && data.committedPoints > 0 ? Math.round((data.completedPoints / data.committedPoints) * 100) : null;
  const left = sprint.state === "active" ? remainingLabel(sprint.endDate) : null;
  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <TrendingDown className="size-4 text-muted" /> Sprint burndown
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
          <>
            <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
              <StatTile label="Committed" value={`${formatPoints(data.committedPoints)} pts`} sub={`${data.committedIssues} issue${data.committedIssues === 1 ? "" : "s"} at sprint start`} />
              <StatTile label="Completed" value={`${formatPoints(data.completedPoints)} pts`} sub={`${data.completedIssues} issue${data.completedIssues === 1 ? "" : "s"} done`} />
              <StatTile label="Completion" value={pct == null ? "–" : `${pct}%`} sub="of committed points" />
              <StatTile label="Remaining" value={`${formatPoints(Math.max(0, data.committedPoints - data.completedPoints))} pts`} sub={left?.text ?? (sprint.state === "closed" ? "Sprint completed" : undefined)} />
            </div>
            {data.burndown.length > 0 ? <BurndownChart report={data} /> : <p className="text-sm text-muted">There's no burndown for this sprint yet.</p>}
          </>
        )}
      </div>
    </Card>
  );
}

function Velocity({ sprints }: { sprints: Sprint[] }) {
  // The last eight completed sprints, oldest first.
  const recent = useMemo(() => [...sprints].sort((a, b) => (a.completedAt ?? "").localeCompare(b.completedAt ?? "")).slice(-8), [sprints]);
  const reports = useQueries({ queries: recent.map((s) => ({ queryKey: ["sprint-report", s.id], ...reportQuery(s.id), staleTime: 5 * 60_000 })) });
  const loading = reports.some((r) => r.isLoading);
  const failed = reports.find((r) => r.error)?.error;
  const data: VelocityPoint[] = reports.flatMap((r, i) =>
    r.data ? [{ id: recent[i].id, name: recent[i].name, committed: r.data.committedPoints, completed: r.data.completedPoints, committedIssues: r.data.committedIssues, completedIssues: r.data.completedIssues }] : [],
  );
  const average = data.length ? data.reduce((n, d) => n + d.completed, 0) / data.length : 0;
  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <BarChart3 className="size-4 text-muted" /> Velocity
        </span>
      }
      actions={data.length > 0 && <span className="text-xs text-muted">Average {formatPoints(Math.round(average * 10) / 10)} points per sprint</span>}
    >
      <div className="p-4">
        {failed ? (
          <ErrorBox error={failed} />
        ) : recent.length === 0 ? (
          <EmptyState title="No completed sprints yet" body="Velocity compares committed and completed story points once sprints are completed." />
        ) : loading ? (
          <PageSpinner label="Loading velocity" />
        ) : (
          <VelocityChart data={data} />
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
