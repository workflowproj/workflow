import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ExternalLink, Inbox, ListFilter, Settings2 } from "lucide-react";
import { hasPermission, useProject } from "@/api/queries";
import { EmptyState, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { formatDateTime, relativeTime } from "@/lib/format";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { RequestTypeIcon, SlaBadge } from "@/features/servicedesk/bits";
import { queueIssuesQuery, queuesQuery, serviceDeskQuery, type Queue, type QueueItem } from "@/features/servicedesk/api";

const route = getRouteApi("/app/projects/$projectKey/queues");

/** QueuesPage is a service project's agent view: its queues with their counts, and the requests in one. */
export function QueuesPage() {
  const { projectKey } = route.useParams();
  const search = route.useSearch();
  const navigate = useNavigate();
  const { data: project } = useProject(projectKey);
  const queues = useQuery(queuesQuery(projectKey));
  const desk = useQuery(serviceDeskQuery(projectKey));
  const list = queues.data ?? [];
  const current = list.find((q) => q.id === search.queue) ?? list[0];
  const go = (next: { queue?: string; issue?: string }) =>
    navigate({ to: "/projects/$projectKey/queues", params: { projectKey }, search: { queue: current?.id, issue: search.issue, ...next } });
  if (queues.isLoading) return <PageSpinner label="Loading queues" />;
  if (queues.error) return <ErrorBox error={queues.error} className="m-6" />;
  return (
    <div className="flex h-full min-h-0 flex-col gap-4 p-4 md:flex-row md:gap-6 md:p-6">
      <nav aria-label="Queues" className="shrink-0 md:w-56">
        <div className="mb-2 flex items-center justify-between gap-2">
          <h2 className="text-[11px] font-semibold tracking-wide text-faint uppercase">Queues</h2>
          {hasPermission(project, "project.admin") && (
            <Link
              to="/projects/$projectKey/settings/queues"
              params={{ projectKey }}
              className="inline-flex items-center gap-1 text-xs text-muted hover:text-ink"
              aria-label="Edit queues"
            >
              <Settings2 aria-hidden className="size-3.5" /> Edit
            </Link>
          )}
        </div>
        <label className="md:hidden">
          <span className="sr-only">Queue</span>
          <Select value={current?.id ?? ""} onChange={(e) => go({ queue: e.target.value })}>
            {list.map((q) => (
              <option key={q.id} value={q.id}>
                {q.name} ({q.count})
              </option>
            ))}
          </Select>
        </label>
        <ul className="hidden flex-col gap-0.5 md:flex">
          {list.map((q) => (
            <li key={q.id}>
              <button
                type="button"
                aria-current={q.id === current?.id ? "page" : undefined}
                onClick={() => go({ queue: q.id })}
                className={cn(
                  "flex w-full items-center justify-between gap-2 rounded-md px-2.5 py-1.5 text-left text-sm",
                  q.id === current?.id ? "bg-accent-soft font-medium text-accent" : "text-muted hover:bg-surface-2 hover:text-ink",
                )}
              >
                <span className="truncate">{q.name}</span>
                <span className={cn("rounded-full px-1.5 text-xs tabular-nums", q.id === current?.id ? "bg-surface text-accent" : "bg-surface-2 text-muted")}>{q.count}</span>
              </button>
            </li>
          ))}
        </ul>
        {desk.data && (
          <a href={desk.data.portalUrl} target="_blank" rel="noreferrer" className="mt-4 hidden items-center gap-1.5 px-2.5 text-xs text-muted hover:text-ink md:flex">
            <ExternalLink aria-hidden className="size-3.5" /> Open the help centre
          </a>
        )}
      </nav>
      <div className="min-w-0 flex-1">
        {current ? (
          <QueueTable queue={current} onOpen={(key) => go({ issue: key })} />
        ) : (
          <div className="rounded-lg border border-line bg-surface">
            <EmptyState
              icon={<ListFilter className="size-8" />}
              title="No queues"
              body="Queues are saved searches of this project's requests. A project admin can add them in the settings."
            />
          </div>
        )}
      </div>
      <IssueDrawer issueKey={search.issue} onClose={() => go({ issue: undefined })} onNavigate={(key) => go({ issue: key })} />
    </div>
  );
}

function QueueTable({ queue, onOpen }: { queue: Queue; onOpen: (key: string) => void }) {
  const q = useQuery({ ...queueIssuesQuery(queue.id), placeholderData: keepPreviousData });
  const items = q.data?.items ?? [];
  return (
    <section aria-labelledby="queue-title" className="@container overflow-hidden rounded-lg border border-line bg-surface">
      <header className="flex flex-wrap items-baseline justify-between gap-2 border-b border-line px-4 py-3">
        <h1 id="queue-title" className="text-base font-semibold">
          {queue.name}
        </h1>
        <p className="text-[13px] text-muted">
          {q.data ? `${q.data.total} request${q.data.total === 1 ? "" : "s"}` : ""}
          <code className="ml-2 hidden font-mono text-xs text-faint @2xl:inline" title={queue.jql}>
            {queue.jql.length > 70 ? queue.jql.slice(0, 70) + "…" : queue.jql}
          </code>
        </p>
      </header>
      {q.error && <ErrorBox error={q.error} className="m-4" />}
      {q.isLoading ? (
        <PageSpinner label="Loading requests" />
      ) : items.length === 0 ? (
        <EmptyState icon={<Inbox className="size-8" />} title="Nothing here" body="No request matches this queue right now." />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full table-fixed border-collapse text-sm">
            <thead>
              <tr className="border-b border-line bg-surface-2/50 text-left text-xs font-semibold text-muted">
                <th scope="col" className="w-10 px-3 py-2">
                  <span className="sr-only">Type</span>
                </th>
                <th scope="col" className="hidden w-28 px-2 py-2 @xl:table-cell">
                  Key
                </th>
                <th scope="col" className="px-2 py-2">
                  Summary
                </th>
                <th scope="col" className="hidden w-36 px-2 py-2 @5xl:table-cell">
                  Reporter
                </th>
                <th scope="col" className="hidden w-44 px-2 py-2 @xl:table-cell">
                  Status
                </th>
                <th scope="col" className="w-12 px-2 py-2 @6xl:w-40">
                  <span className="sr-only @6xl:not-sr-only">Assignee</span>
                </th>
                <th scope="col" className="hidden w-52 px-2 py-2 @2xl:table-cell">
                  SLAs
                </th>
                <th scope="col" className="hidden w-28 px-2 py-2 @6xl:table-cell">
                  Created
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <Row key={it.issue.id} item={it} onOpen={onOpen} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function Row({ item, onOpen }: { item: QueueItem; onOpen: (key: string) => void }) {
  const i = item.issue;
  return (
    <tr
      tabIndex={0}
      onClick={() => onOpen(i.key)}
      onKeyDown={(e) => e.key === "Enter" && onOpen(i.key)}
      className="cursor-pointer border-b border-line last:border-b-0 hover:bg-surface-2 focus-visible:bg-surface-2 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
    >
      <td className="px-3 py-2.5">{item.requestType ? <RequestTypeIcon icon={item.requestType.icon} size="sm" /> : <IssueTypeIcon type={i.type} />}</td>
      <td className="hidden px-2 py-2.5 font-mono text-xs text-muted @xl:table-cell">
        <Link to="/browse/$issueKey" params={{ issueKey: i.key }} onClick={(e) => (e.preventDefault(), onOpen(i.key))} className="hover:text-accent hover:underline">
          {i.key}
        </Link>
      </td>
      <td className="px-2 py-2.5">
        <p className="truncate font-medium">{i.summary}</p>
        <p className="truncate text-xs text-muted">
          <span className="font-mono @xl:hidden">{i.key} · </span>
          {item.requestType?.name}
        </p>
        {/* Narrow queues show the clocks under the summary. */}
        <span className="mt-1 flex flex-wrap gap-1 @2xl:hidden">
          {item.slas.map((s) => (
            <SlaBadge key={s.metric} sla={s} short />
          ))}
        </span>
      </td>
      <td className="hidden px-2 py-2.5 @5xl:table-cell">
        <span className="block truncate text-[13px]">{i.reporter?.displayName ?? "—"}</span>
      </td>
      <td className="hidden px-2 py-2.5 @xl:table-cell">
        <StatusBadge status={i.status} />
      </td>
      <td className="px-2 py-2.5">
        <span className="flex items-center gap-2">
          <UserAvatar user={i.assignee} size={22} />
          <span className="hidden truncate text-[13px] @6xl:inline">{i.assignee?.displayName ?? "Unassigned"}</span>
        </span>
      </td>
      <td className="hidden px-2 py-2.5 @2xl:table-cell">
        <span className="flex flex-col items-start gap-1">
          {item.slas.length === 0 && <span className="text-xs text-faint">—</span>}
          {item.slas.map((s) => (
            <SlaBadge key={s.metric} sla={s} short />
          ))}
        </span>
      </td>
      <td className="hidden px-2 py-2.5 text-[13px] text-muted @6xl:table-cell">
        <time dateTime={i.createdAt} title={formatDateTime(i.createdAt)}>
          {relativeTime(i.createdAt)}
        </time>
      </td>
    </tr>
  );
}
