import { useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { Layers, Timer, TrendingUp, Users } from "lucide-react";
import { Card, EmptyState, PageHeader, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { breakdownQuery, createdResolvedQuery, cycleTimeQuery, flowQuery, summaryQuery, type MetricScope } from "@/features/dashboards/api";
import { breakdownClause, scopeJql } from "@/features/dashboards/model";
import { BreakdownBars, type BreakdownRow } from "@/features/reports/BreakdownChart";
import { StatTile, TruncatedNote } from "@/features/reports/ChartBits";
import { CreatedResolvedChart } from "@/features/reports/CreatedResolvedChart";
import { CumulativeFlowChart } from "@/features/reports/CumulativeFlowChart";
import { CycleTimeChart } from "@/features/reports/CycleTimeChart";
import { formatDuration } from "@/features/reports/chart-utils";
import { programmeTeamsQuery } from "./queries";

const WINDOWS = [14, 30, 90, 180];

/**
 * ProgrammeReports is flow across a programme: its own issues and its teams' (those the viewer can
 * browse), with work by team, cumulative flow, created vs resolved and cycle time. Velocity stays
 * with each team, whose sprints differ.
 */
export function ProgrammeReports({ programmeKey }: { programmeKey: string }) {
  const { data: teams, isLoading, error } = useQuery(programmeTeamsQuery(programmeKey));
  const [days, setDays] = useState(30);
  const navigate = useNavigate();
  const keys = [programmeKey, ...(teams?.teams ?? []).map((t) => t.key)];
  const scope: MetricScope = { projectKeys: keys };
  const ready = { enabled: !!teams };
  const byTeam = useQuery({ ...breakdownQuery({ ...scope, jql: "statusCategory != Done" }, "project"), ...ready });
  const flow = useQuery({ ...flowQuery(scope, days, "category"), ...ready });
  const createdResolved = useQuery({ ...createdResolvedQuery(scope, days, days > 30 ? "week" : "day"), ...ready });
  const cycle = useQuery({ ...cycleTimeQuery(scope, days), ...ready });
  const window = (
    <div className="w-40">
      <Select aria-label="Time window" value={days} onChange={(e) => setDays(Number(e.target.value))} className="h-8 text-[13px]">
        {WINDOWS.map((d) => (
          <option key={d} value={d}>
            Last {d} days
          </option>
        ))}
      </Select>
    </div>
  );
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6 p-4 md:p-6">
      <PageHeader
        title="Reports"
        subtitle={
          teams
            ? `Flow across the programme and ${teams.teams.length === 1 ? "its team" : `its ${teams.teams.length} teams`}${teams.hiddenCount ? ` (${teams.hiddenCount} you can't browse left out)` : ""}. Velocity stays with each team.`
            : "Flow across the programme and its teams."
        }
        actions={window}
      />
      {error && <ErrorBox error={error} />}
      {isLoading || !teams ? (
        !error && <PageSpinner label="Loading reports" />
      ) : (
        <>
          <Summary scope={scope} days={days} />
          <Chart icon={<Users className="size-4 text-muted" />} title="Open work by team" query={byTeam}>
            {(d) =>
              d.total === 0 ? (
                <EmptyState title="No open work" body="Nothing in the programme or its teams is open." />
              ) : (
                <BreakdownBars
                  rows={d.groups.map((g): BreakdownRow => ({ key: g.key, label: g.label, value: g.count }))}
                  total={d.total}
                  unit="issues"
                  format={String}
                  jqlFor={(r) => scopeJql(keys, "statusCategory != Done", breakdownClause("project", r.key))}
                  label="Open issues by team"
                />
              )
            }
          </Chart>
          <Chart icon={<Layers className="size-4 text-muted" />} title="Cumulative flow" query={flow}>
            {(d) => (
              <>
                <CumulativeFlowChart data={d} />
                {d.truncated && <TruncatedNote what="created" />}
              </>
            )}
          </Chart>
          <Chart icon={<TrendingUp className="size-4 text-muted" />} title="Created vs resolved" query={createdResolved}>
            {(d) => <CreatedResolvedChart data={d} />}
          </Chart>
          <Chart icon={<Timer className="size-4 text-muted" />} title="Cycle time" query={cycle}>
            {(d) =>
              d.issues.length === 0 ? (
                <EmptyState title={`Nothing resolved in the last ${days} days`} body="Cycle time appears as issues are resolved." />
              ) : (
                <>
                  <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-3">
                    <StatTile label="Median" value={formatDuration(d.medianHours)} sub="half of issues are faster" />
                    <StatTile label="85th percentile" value={formatDuration(d.p85Hours)} sub="most issues finish within" />
                    <StatTile label="Resolved" value={d.issues.length} sub={`in the last ${days} days`} />
                  </div>
                  <CycleTimeChart report={d} onOpen={(key) => navigate({ to: "/browse/$issueKey", params: { issueKey: key } })} />
                </>
              )
            }
          </Chart>
        </>
      )}
    </div>
  );
}

function Summary({ scope, days }: { scope: MetricScope; days: number }) {
  const { data, error } = useQuery(summaryQuery(scope, days));
  if (error) return <ErrorBox error={error} />;
  if (!data) return null;
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <StatTile label="Open" value={data.open} sub={`${data.inProgress} in progress`} />
      <StatTile label="Created" value={data.created} sub={`in the last ${days} days`} />
      <StatTile label="Resolved" value={data.resolved} sub={`in the last ${days} days`} />
      <StatTile label="Overdue" value={data.overdue} sub="open past their due date" />
    </div>
  );
}

function Chart<T>({ icon, title, query, children }: { icon: ReactNode; title: string; query: UseQueryResult<T>; children: (data: T) => ReactNode }) {
  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          {icon} {title}
        </span>
      }
    >
      <div className={`p-4 transition-opacity ${query.isPlaceholderData ? "opacity-60" : ""}`}>
        {query.error ? <ErrorBox error={query.error} /> : query.data === undefined ? <PageSpinner label={`Loading ${title.toLowerCase()}`} /> : children(query.data)}
      </div>
    </Card>
  );
}

