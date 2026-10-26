import { useCallback, useMemo, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Plus, Search, ShieldAlert, X } from "lucide-react";
import { hasPermission, issueTypesQuery, useProject } from "@/api/queries";
import type { Project } from "@/api/types";
import { Button, EmptyState, PageHeader, PageSpinner, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { Segmented, StatTile } from "@/features/reports/ChartBits";
import { riskRegisterQuery, type RiskEntry } from "@/features/risks/api";
import { RatingBadge, RiskMatrix, WorkProgress } from "@/features/risks/bits";
import { IMPACT, LIKELIHOOD, isOverdue, parseCell, riskJql } from "@/features/risks/model";
import { todayISO } from "@/features/backlog/dates";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";

const route = getRouteApi("/app/projects/$projectKey/risks");

export type RiskFilter = "open" | "closed" | "all";

/** RisksPage is the project's risk register: totals, the likelihood × impact matrix and the risks. */
export function RisksPage() {
  const { projectKey } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const { data: project } = useProject(projectKey);
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  return (
    <div className="mx-auto max-w-6xl p-4 md:p-6">
      {project ? <Register project={project} onOpen={openIssue} /> : <PageSpinner />}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

function Register({ project, onOpen }: { project: Project; onOpen: (key: string) => void }) {
  const { cell: cellParam, status = "open" } = route.useSearch();
  const navigate = route.useNavigate();
  const { data, isLoading, error } = useQuery(riskRegisterQuery(project.key));
  const { data: types = [] } = useQuery(issueTypesQuery(project.key));
  const [creating, setCreating] = useState(false);
  const riskType = types.find((t) => t.kind === "risk");
  const canCreate = hasPermission(project, "issue.create") && !!riskType;
  const cell = parseCell(cellParam);
  const today = todayISO();

  const shown = useMemo(() => {
    const list = (data?.risks ?? []).filter((r) => {
      const open = r.issue.status.category !== "done";
      if (status === "open" && !open) return false;
      if (status === "closed" && open) return false;
      return !cell || (open && r.likelihood === cell.likelihood && r.impact === cell.impact);
    });
    // Highest score first, unassessed last, then newest first (the register's order).
    return list.sort((a, b) => (b.score ?? 0) - (a.score ?? 0));
  }, [data, status, cell]);

  // Open is the default, so it stays out of the URL.
  const setSearch = (s: { cell?: string; status?: RiskFilter }) =>
    navigate({
      search: (prev) => ({ ...prev, cell: "cell" in s ? s.cell : prev.cell, status: !("status" in s) ? prev.status : s.status === "open" ? undefined : s.status }),
      replace: true,
    });
  const newRisk = canCreate && (
    <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
      New risk
    </Button>
  );

  if (isLoading) return <PageSpinner label="Loading risks" />;
  return (
    <>
      <PageHeader
        title="Risks"
        subtitle="What might go wrong, how likely and how bad it would be, who owns it and the work that mitigates it."
        actions={
          <>
            <Link to="/issues" search={{ jql: riskJql(project.key, cell) }} className="inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-ink">
              <Search className="size-3.5" aria-hidden /> Open in search
            </Link>
            {newRisk}
          </>
        }
      />
      {error && <ErrorBox error={error} />}
      {data && data.risks.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<ShieldAlert className="size-8" />}
            title="No risks yet"
            body="Record what might go wrong: how likely it is, how bad it would be and who owns it. Link the work that mitigates each one."
            action={newRisk}
          />
        </div>
      ) : (
        data && (
          <div className="flex flex-col gap-5">
            <div className="grid gap-3 sm:grid-cols-3">
              <StatTile label="Open risks" value={data.summary.open} sub={data.summary.unassessed ? `${data.summary.unassessed} not assessed yet` : undefined} />
              <StatTile label="High and critical" value={data.summary.highOrCritical} sub="open risks scoring 10 or more" />
              <StatTile label="Reviews overdue" value={data.summary.reviewsOverdue} sub="open risks past their review date" />
            </div>
            <section aria-labelledby="matrix-title" className="rounded-lg border border-line bg-surface p-4">
              <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                <h2 id="matrix-title" className="text-sm font-semibold">
                  Likelihood × impact
                </h2>
                <p className="text-xs text-muted">Open risks. Pick a cell to list its risks.</p>
              </div>
              <div className="max-w-2xl">
                <RiskMatrix cells={data.matrix} selected={cell} onSelect={(c) => setSearch({ cell: c ? `${c.likelihood}-${c.impact}` : undefined, status: c ? "open" : status })} />
              </div>
            </section>
            <section aria-labelledby="register-title" className="rounded-lg border border-line bg-surface">
              <div className="flex flex-wrap items-center gap-2 border-b border-line p-2 pl-4">
                <h2 id="register-title" className="mr-auto text-sm font-semibold">
                  Register
                </h2>
                {cell && (
                  <span className="inline-flex h-7 items-center gap-1 rounded-md bg-accent-soft pr-1 pl-2.5 text-[13px] text-accent">
                    {LIKELIHOOD[cell.likelihood - 1]} × {IMPACT[cell.impact - 1]}
                    <button type="button" aria-label="Show every cell" onClick={() => setSearch({ cell: undefined })} className="rounded p-0.5 hover:bg-accent/10">
                      <X className="size-3.5" />
                    </button>
                  </span>
                )}
                <Segmented<RiskFilter>
                  label="Risk status"
                  value={status}
                  onChange={(v) => setSearch({ status: v, cell: v === "open" ? cellParam : undefined })}
                  options={[
                    { value: "open", label: "Open" },
                    { value: "closed", label: "Closed" },
                    { value: "all", label: "All" },
                  ]}
                />
              </div>
              {shown.length === 0 ? (
                <p className="px-4 py-8 text-center text-sm text-muted">No {status === "all" ? "" : status} risks{cell ? " in that cell" : ""}.</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className={table}>
                    <thead>
                      <tr>
                        <th>Key</th>
                        <th>Risk</th>
                        <th>Score</th>
                        <th>Owner</th>
                        <th>Status</th>
                        <th>Review date</th>
                        <th>Mitigating work</th>
                      </tr>
                    </thead>
                    <tbody>
                      {shown.map((r) => (
                        <RiskRow key={r.issue.id} risk={r} today={today} onOpen={onOpen} />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              {data.truncated && <p className="border-t border-line px-4 py-2 text-xs text-muted">Showing the 2,000 newest risks. Search for the rest.</p>}
            </section>
          </div>
        )
      )}
      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={project.key} defaults={{ typeKey: riskType?.key }} />
    </>
  );
}

function RiskRow({ risk: r, today, onOpen }: { risk: RiskEntry; today: string; onOpen: (key: string) => void }) {
  const i = r.issue;
  const overdue = isOverdue(i.dueDate, i.status.category, today);
  const open = (e: React.MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    onOpen(i.key);
  };
  return (
    <tr className="hover:bg-surface-2/60">
      <td className="font-mono text-xs whitespace-nowrap">
        <Link to="/browse/$issueKey" params={{ issueKey: i.key }} onClick={open} className="text-muted hover:text-accent hover:underline">
          {i.key}
        </Link>
      </td>
      <td className="min-w-48">
        <Link to="/browse/$issueKey" params={{ issueKey: i.key }} onClick={open} className="hover:text-accent hover:underline">
          {i.summary}
        </Link>
        {r.response && <span className="ml-2 text-xs text-muted">{r.response}</span>}
      </td>
      <td>
        <RatingBadge score={r.score} rating={r.rating} />
      </td>
      <td className="whitespace-nowrap">
        {i.assignee ? (
          <span className="inline-flex items-center gap-1.5">
            <UserAvatar user={i.assignee} size={20} />
            {i.assignee.displayName}
          </span>
        ) : (
          <span className="text-faint">No owner</span>
        )}
      </td>
      <td>
        <StatusBadge status={i.status} />
      </td>
      <td className={cn("whitespace-nowrap", overdue && "font-medium text-danger")}>
        {i.dueDate ? (
          <>
            {formatDate(i.dueDate)}
            {overdue && <span className="sr-only"> (overdue)</span>}
          </>
        ) : (
          <span className="text-faint">—</span>
        )}
      </td>
      <td>
        <WorkProgress total={r.mitigation.total} done={r.mitigation.done} />
      </td>
    </tr>
  );
}
