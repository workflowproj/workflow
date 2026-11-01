import { useCallback, useMemo, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { EyeOff, Flag, Plus } from "lucide-react";
import { hasPermission, issueTypesQuery, projectsQuery, useProject } from "@/api/queries";
import { Button, EmptyState, PageHeader, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { Chip } from "@/features/timeline/Toolbar";
import { StatTile } from "@/features/reports/ChartBits";
import { roadmapTotals } from "@/features/programme/model";
import { programmeOverviewQuery } from "@/features/programme/queries";
import { InitiativeCard, UnlinkedEpics } from "@/features/programme/Roadmap";
import { useLiveEvents } from "@/lib/events";
import { cn } from "@/lib/cn";

const route = getRouteApi("/app/projects/$projectKey/roadmap");

/**
 * ProgrammeRoadmapPage is a programme's roadmap: its initiatives with the team epics under them,
 * how far along the work is, forecasts and flags, and team epics that belong to no initiative.
 */
export function ProgrammeRoadmapPage() {
  const { projectKey } = route.useParams();
  const { issue, done } = route.useSearch();
  const navigate = route.useNavigate();
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  const { data: project } = useProject(projectKey);
  const { data, isLoading, error, isPlaceholderData } = useQuery(programmeOverviewQuery(projectKey, done ? 90 : 0));
  // ProjectLayout follows the programme; the roadmap also follows the teams it shows.
  useLiveEvents(data?.teams.length ? data.teams.map((t) => t.key) : undefined, { debounceMs: 500 });
  const { data: projects = [] } = useQuery(projectsQuery);
  const editable = useMemo(() => new Set(projects.filter((p) => p.myPermissions.includes("issue.edit")).map((p) => p.key)), [projects]);
  const { data: types = [] } = useQuery(issueTypesQuery(projectKey));
  const initiativeType = types.find((t) => t.level === 2 && t.kind === "work");
  const canCreate = !!project && hasPermission(project, "issue.create") && !!initiativeType;
  const isAdmin = !!project && hasPermission(project, "project.admin");
  const [creating, setCreating] = useState(false);
  const totals = data ? roadmapTotals(data) : undefined;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-5 p-4 md:p-6">
      <PageHeader
        title="Roadmap"
        subtitle="The programme's initiatives and the team epics working toward them."
        actions={
          canCreate && (
            <Button variant="primary" icon={<Plus className="size-4" aria-hidden />} onClick={() => setCreating(true)}>
              New initiative
            </Button>
          )
        }
      />
      {error && <ErrorBox error={error} />}
      {isLoading || !data || !totals ? (
        !error && <PageSpinner label="Loading roadmap" />
      ) : (
        <div className={cn("flex flex-col gap-5 transition-opacity", isPlaceholderData && "opacity-70")}>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatTile label="Open initiatives" value={totals.open} sub={`${totals.initiatives} shown`} />
            <StatTile label="Epics" value={totals.epics} sub={`in ${data.teams.length === 1 ? "1 team" : `${data.teams.length} teams`}`} />
            <StatTile label="Late epics" value={totals.late} sub="forecast after the initiative is due" />
            <StatTile label="Blocked epics" value={totals.blocked} sub="by unfinished work in another project" />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Chip pressed={!!done} onClick={() => navigate({ search: (s) => ({ ...s, done: done ? undefined : true }) })} title="Include initiatives finished in the last 90 days">
              Show done
            </Chip>
            {data.hiddenTeamCount > 0 && (
              <p className="inline-flex items-center gap-1.5 text-[13px] text-muted">
                <EyeOff className="size-3.5" aria-hidden />
                {data.hiddenTeamCount === 1 ? "1 team" : `${data.hiddenTeamCount} teams`} you can't browse {data.hiddenTeamCount === 1 ? "is" : "are"} left out: progress counts only what you can see.
              </p>
            )}
          </div>
          {data.truncated && <p className="text-[13px] text-warning">The roadmap shows the first 500 initiatives and 2,000 epics only.</p>}
          {data.teams.length === 0 && data.hiddenTeamCount === 0 ? (
            <EmptyState
              icon={<Flag className="size-8" />}
              title="No teams yet"
              body="Add the team projects whose epics deliver this programme's initiatives."
              action={
                isAdmin && (
                  <Link to="/projects/$projectKey/settings/teams" params={{ projectKey }} className="text-sm font-medium text-accent hover:underline">
                    Add teams in settings
                  </Link>
                )
              }
            />
          ) : data.initiatives.length === 0 ? (
            <EmptyState
              icon={<Flag className="size-8" />}
              title={done ? "No initiatives yet" : "No open initiatives"}
              body="An initiative is an outcome the teams work toward together. Create one, then give team epics this initiative."
            />
          ) : (
            <div className="flex flex-col gap-3">
              {data.initiatives.map((i) => (
                <InitiativeCard key={i.issue.key} item={i} teams={data.teams} onOpen={openIssue} />
              ))}
            </div>
          )}
          <UnlinkedEpics epics={data.unlinkedEpics} total={data.unlinkedTotal} initiatives={data.initiatives} canEdit={(k) => editable.has(k)} onOpen={openIssue} />
        </div>
      )}
      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={projectKey} defaults={{ typeKey: initiativeType?.key }} />
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}
