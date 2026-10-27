import { useCallback, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Plus, Scale } from "lucide-react";
import { hasPermission, issueTypesQuery, useProject } from "@/api/queries";
import type { Project } from "@/api/types";
import { Button, EmptyState, PageHeader, PageSpinner, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { StatusBadge } from "@/components/issue-bits";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { decisionLogQuery, type DecisionEntry } from "@/features/risks/api";
import { WorkProgress } from "@/features/risks/bits";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";

const route = getRouteApi("/app/projects/$projectKey/decisions");

/** DecisionsPage is the project's decision log: what was decided, when, and what replaced it. */
export function DecisionsPage() {
  const { projectKey } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const { data: project } = useProject(projectKey);
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  return (
    <div className="mx-auto max-w-6xl p-4 md:p-6">
      {project ? <Log project={project} onOpen={openIssue} /> : <PageSpinner />}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

function Log({ project, onOpen }: { project: Project; onOpen: (key: string) => void }) {
  const { status } = route.useSearch();
  const navigate = route.useNavigate();
  const { data, isLoading, error } = useQuery(decisionLogQuery(project.key));
  const { data: types = [] } = useQuery(issueTypesQuery(project.key));
  const [creating, setCreating] = useState(false);
  const decisionType = types.find((t) => t.kind === "decision");
  const canCreate = hasPermission(project, "issue.create") && !!decisionType;
  const decisions = data?.decisions ?? [];
  // The statuses decisions are in, in workflow order (to do, in progress, done), for the filter.
  const order = { todo: 0, in_progress: 1, done: 2 };
  const statuses = [...new Map(decisions.map((d) => [d.issue.status.name, d.issue.status])).values()].sort((a, b) => order[a.category] - order[b.category]);
  const shown = status ? decisions.filter((d) => d.issue.status.name === status) : decisions;
  const setStatus = (s?: string) => navigate({ search: (prev) => ({ ...prev, status: s }), replace: true });
  const newDecision = canCreate && (
    <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
      New decision
    </Button>
  );

  if (isLoading) return <PageSpinner label="Loading decisions" />;
  return (
    <>
      <PageHeader title="Decisions" subtitle="The decisions that shape the work, why they were made, and the ones that replaced them." actions={newDecision} />
      {error && <ErrorBox error={error} />}
      {data && decisions.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<Scale className="size-8" />}
            title="No decisions yet"
            body="Record a decision with its context, the options considered and its consequences. Link the work that implements it, and supersede it when it changes."
            action={newDecision}
          />
        </div>
      ) : (
        data && (
          <section aria-label="Decision log" className="rounded-lg border border-line bg-surface">
            <div role="tablist" aria-label="Decision status" className="flex flex-wrap gap-1 border-b border-line p-2">
              {[{ name: "All", value: undefined, count: decisions.length }, ...statuses.map((s) => ({ name: s.name, value: s.name, count: decisions.filter((d) => d.issue.status.name === s.name).length }))].map((f) => (
                <button
                  key={f.name}
                  type="button"
                  role="tab"
                  aria-selected={status === f.value}
                  onClick={() => setStatus(f.value)}
                  className={cn("inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-[13px] font-medium", status === f.value ? "bg-accent-soft text-accent" : "text-muted hover:bg-surface-2 hover:text-ink")}
                >
                  {f.name}
                  <span className="text-xs opacity-75 tabular-nums">{f.count}</span>
                </button>
              ))}
            </div>
            {shown.length === 0 ? (
              <p className="px-4 py-8 text-center text-sm text-muted">No {status ? status.toLowerCase() : ""} decisions.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className={table}>
                  <thead>
                    <tr>
                      <th>Key</th>
                      <th>Decision</th>
                      <th>Status</th>
                      <th>Decided on</th>
                      <th>Superseded by</th>
                      <th>Implementing work</th>
                    </tr>
                  </thead>
                  <tbody>
                    {shown.map((d) => (
                      <DecisionRow key={d.issue.id} decision={d} onOpen={onOpen} />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {data.truncated && <p className="border-t border-line px-4 py-2 text-xs text-muted">Showing the 2,000 newest decisions. Search for the rest.</p>}
          </section>
        )
      )}
      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={project.key} defaults={{ typeKey: decisionType?.key }} />
    </>
  );
}

function DecisionRow({ decision: d, onOpen }: { decision: DecisionEntry; onOpen: (key: string) => void }) {
  const open = (key: string) => (e: React.MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    onOpen(key);
  };
  const i = d.issue;
  return (
    <tr className="hover:bg-surface-2/60">
      <td className="font-mono text-xs whitespace-nowrap">
        <Link to="/browse/$issueKey" params={{ issueKey: i.key }} onClick={open(i.key)} className="text-muted hover:text-accent hover:underline">
          {i.key}
        </Link>
      </td>
      <td className="min-w-56">
        <Link to="/browse/$issueKey" params={{ issueKey: i.key }} onClick={open(i.key)} className="hover:text-accent hover:underline">
          {i.summary}
        </Link>
        {d.supersedes.length > 0 && <span className="block text-xs text-muted">Supersedes {d.supersedes.map((s) => s.key).join(", ")}</span>}
      </td>
      <td>
        <StatusBadge status={i.status} />
      </td>
      <td className="whitespace-nowrap">{d.decidedOn ? formatDate(d.decidedOn) : <span className="text-faint">—</span>}</td>
      <td>
        {d.supersededBy.length === 0 ? (
          <span className="text-faint">—</span>
        ) : (
          <span className="flex flex-col gap-0.5">
            {d.supersededBy.map((s) => (
              <Link key={s.id} to="/browse/$issueKey" params={{ issueKey: s.key }} onClick={open(s.key)} className="text-[13px] hover:text-accent hover:underline">
                <span className="font-mono text-xs text-muted">{s.key}</span> {s.summary}
              </Link>
            ))}
          </span>
        )}
      </td>
      <td>
        <WorkProgress total={d.implementation.total} done={d.implementation.done} />
      </td>
    </tr>
  );
}
