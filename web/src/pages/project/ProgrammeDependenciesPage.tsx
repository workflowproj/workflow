import { useCallback, useMemo } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, EyeOff, GitFork } from "lucide-react";
import { Card, EmptyState, PageHeader, PageSpinner, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { crossTeamLinks, dependencyMatrix, type CrossTeamLink } from "@/features/programme/model";
import { dependencies, formatDay, schedules, toDay } from "@/features/timeline/model";
import { timelineQuery } from "@/features/timeline/queries";
import { ProjectAvatar } from "@/layout/AppShell";
import { useLiveEvents } from "@/lib/events";
import { cn } from "@/lib/cn";
import type { Schemas } from "@/api/client";

const route = getRouteApi("/app/projects/$projectKey/dependencies");

type Issue = Schemas["Issue"];

/**
 * ProgrammeDependenciesPage shows who waits on whom across a programme's teams: a matrix of
 * "blocks" links between projects, the links with the dates that clash, and the work the
 * programme's issues wait on (or hold up) outside it.
 */
export function ProgrammeDependenciesPage() {
  const { projectKey } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  // The same data as the programme's timeline (and its cache), with done work left out.
  const { data, isLoading, error } = useQuery(timelineQuery("", "", 0, projectKey));
  useLiveEvents(data ? data.projects.filter((p) => p.key !== projectKey).map((p) => p.key) : undefined, { debounceMs: 500 });
  const links = useMemo(() => (data ? crossTeamLinks(data) : []), [data]);
  const conflicts = useMemo(() => {
    if (!data) return new Set<string>();
    const { deps } = dependencies(data, schedules(data));
    return new Set(deps.filter((d) => d.conflict).map((d) => d.link.id));
  }, [data]);
  const matrix = useMemo(() => dependencyMatrix(links), [links]);
  const external = data?.externalLinks ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-5 p-4 md:p-6">
      <PageHeader title="Dependencies" subtitle="Unfinished work in one team that blocks work in another, and what the programme waits on outside it." />
      {error && <ErrorBox error={error} />}
      {isLoading || !data ? (
        !error && <PageSpinner label="Loading dependencies" />
      ) : (
        <>
          {data.truncated && <p className="text-[13px] text-warning">Only the first 2,000 issues are included; some dependencies may be missing.</p>}
          {links.length === 0 && external.length === 0 ? (
            <EmptyState
              icon={<GitFork className="size-8" />}
              title="No dependencies between teams"
              body="When an issue in one team blocks an issue in another (Link issue → blocks), it appears here."
            />
          ) : (
            <>
              {links.length > 0 && <Matrix projects={data.projects} matrix={matrix} conflicts={conflicts} />}
              {links.length > 0 && (
                <Card title={`Between teams (${links.length})`}>
                  <LinkTable links={links} conflicts={conflicts} onOpen={openIssue} />
                </Card>
              )}
              {external.length > 0 && <External items={external} onOpen={openIssue} />}
            </>
          )}
        </>
      )}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

/** Matrix counts blocks links by blocking team (rows) and blocked team (columns). */
function Matrix({ projects, matrix, conflicts }: { projects: Schemas["ProjectRef"][]; matrix: Map<string, Map<string, CrossTeamLink[]>>; conflicts: Set<string> }) {
  const keys = projects.map((p) => p.key).filter((k) => matrix.has(k) || [...matrix.values()].some((row) => row.has(k)));
  return (
    <Card title="Who blocks whom">
      <div className="overflow-x-auto p-4">
        <p className="mb-3 text-[13px] text-muted">Rows block columns: each cell counts the unfinished issues of one team that block another's. Red cells have dates that clash.</p>
        <table className="border-separate border-spacing-1 text-sm">
          <caption className="sr-only">Blocking issues between teams: the blocking team in each row, the blocked team in each column</caption>
          <thead>
            <tr>
              <td />
              {keys.map((k) => (
                <th key={k} scope="col" className="px-2 py-1 text-xs font-semibold text-muted">
                  <span className="inline-flex items-center gap-1.5">
                    <ProjectAvatar projectKey={k} size={16} /> {k}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {keys.map((from) => (
              <tr key={from}>
                <th scope="row" className="px-2 py-1 text-left text-xs font-semibold text-muted">
                  <span className="inline-flex items-center gap-1.5">
                    <ProjectAvatar projectKey={from} size={16} /> {from} blocks
                  </span>
                </th>
                {keys.map((to) => {
                  const cell = matrix.get(from)?.get(to) ?? [];
                  const clash = cell.filter((l) => conflicts.has(l.link.id)).length;
                  return (
                    <td
                      key={to}
                      data-cell={`${from}>${to}`}
                      className={cn(
                        "h-10 min-w-14 rounded text-center tabular-nums",
                        from === to ? "bg-surface-2/50" : cell.length === 0 ? "bg-surface-2 text-faint" : clash ? "bg-danger-soft font-semibold text-danger" : "bg-accent-soft font-semibold text-accent",
                      )}
                    >
                      {from === to ? "" : cell.length || "–"}
                      {clash > 0 && <span className="sr-only">, {clash} with clashing dates</span>}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function IssueCell({ issue, onOpen }: { issue: Issue; onOpen: (key: string) => void }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <IssueTypeIcon type={issue.type} />
      <button type="button" onClick={() => onOpen(issue.key)} className="shrink-0 font-mono text-xs text-muted hover:text-accent hover:underline">
        {issue.key}
      </button>
      <span className="min-w-0 truncate">{issue.summary}</span>
      <StatusBadge status={issue.status} className="shrink-0" />
    </span>
  );
}

function LinkTable({ links, conflicts, onOpen }: { links: CrossTeamLink[]; conflicts: Set<string>; onOpen: (key: string) => void }) {
  const sorted = [...links].sort((a, b) => Number(conflicts.has(b.link.id)) - Number(conflicts.has(a.link.id)));
  return (
    <div className="overflow-x-auto">
      <table className={table}>
        <thead>
          <tr>
            <th scope="col">Blocking</th>
            <th scope="col" className="w-8">
              <span className="sr-only">blocks</span>
            </th>
            <th scope="col">Blocked</th>
            <th scope="col">Dates</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((l) => (
            <tr key={l.link.id}>
              <td className="max-w-80">
                <IssueCell issue={l.from} onOpen={onOpen} />
              </td>
              <td>
                <ArrowRight className="size-4 text-faint" aria-hidden />
              </td>
              <td className="max-w-80">
                <IssueCell issue={l.to} onOpen={onOpen} />
              </td>
              <td className="text-[13px] whitespace-nowrap">
                {conflicts.has(l.link.id) ? (
                  <span className="font-medium text-danger">Clash: {l.from.key} ends after {l.to.key} starts</span>
                ) : (
                  <span className="text-muted">{l.from.dueDate ? `${l.from.key} due ${formatDay(toDay(l.from.dueDate))}` : "No clash"}</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** External lists blocks links to work outside the programme; issues the viewer can't see stay unnamed. */
function External({ items, onOpen }: { items: Schemas["ExternalDependency"][]; onOpen: (key: string) => void }) {
  const open = items.filter((x) => x.open);
  return (
    <Card title={`Outside the programme (${open.length} open)`}>
      <ul className="divide-y divide-line">
        {open.map((x, n) => (
          <li key={`${x.issueKey}-${x.linkId ?? n}`} className="flex flex-col gap-1 px-4 py-2.5 text-sm sm:flex-row sm:items-center sm:gap-3">
            <button type="button" onClick={() => onOpen(x.issueKey)} className="shrink-0 font-mono text-xs text-muted hover:text-accent hover:underline">
              {x.issueKey}
            </button>
            <span className="shrink-0 text-muted">{x.direction === "blockedBy" ? "is blocked by" : "blocks"}</span>
            {x.other ? (
              <IssueCell issue={x.other} onOpen={onOpen} />
            ) : (
              <span className="inline-flex items-center gap-1.5 text-muted">
                <EyeOff className="size-3.5" aria-hidden /> an unfinished issue in a project you can't browse
              </span>
            )}
          </li>
        ))}
      </ul>
      {open.length === 0 && <p className="px-4 py-3 text-[13px] text-muted">Everything the programme waited on outside it is done.</p>}
    </Card>
  );
}
