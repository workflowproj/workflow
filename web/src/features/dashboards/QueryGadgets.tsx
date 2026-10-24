import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChartNoAxesColumn, Inbox, ListX, Timer } from "lucide-react";
import { PriorityIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { formatDay, todayISO } from "@/features/backlog/dates";
import { formatPoints } from "@/features/board/model";
import { BreakdownBars, BreakdownDonut, type BreakdownRow } from "@/features/reports/BreakdownChart";
import { CreatedResolvedChart } from "@/features/reports/CreatedResolvedChart";
import { CumulativeFlowChart } from "@/features/reports/CumulativeFlowChart";
import { CycleTimeChart } from "@/features/reports/CycleTimeChart";
import { formatDuration } from "@/features/reports/chart-utils";
import { cn } from "@/lib/cn";
import { breakdownQuery, createdResolvedQuery, cycleTimeQuery, flowQuery, issueListQuery, summaryQuery, type MetricScope } from "./api";
import { GadgetBody, GadgetFrame, GadgetMessage, SearchLink, useGadget } from "./GadgetFrame";
import { breakdownClause, breakdownLabel, formatStat, gadgetTitle, scopeJql, scopeSummary, searchJql, statCaption, statMeta, statValue } from "./model";

const CHART_H = 220;

/** useScope reads a query gadget's projects and JQL, its title, subtitle and search link. */
function useScope() {
  const ctx = useGadget();
  const { gadget } = ctx;
  const scope: MetricScope = { projectKeys: gadget.config.projectKeys, jql: gadget.config.jql };
  const jql = searchJql(gadget) ?? "";
  return { ...ctx, scope, days: gadget.config.days ?? 30, frame: { title: gadgetTitle(gadget), subtitle: scopeSummary(gadget), link: <SearchLink jql={jql} /> } };
}

const nothing = (body = "Nothing matches this gadget's projects and query.") => <GadgetMessage icon={<Inbox className="size-6" />} title="No issues" body={body} />;

export function FlowGadget() {
  const { gadget, scope, days, frame } = useScope();
  const q = useQuery(flowQuery(scope, days, gadget.config.flowBy ?? "category"));
  return (
    <GadgetFrame {...frame} dimmed={q.isPlaceholderData}>
      <GadgetBody query={q} empty={(d) => d.series.every((s) => s.counts.every((c) => c === 0)) && nothing()}>
        {(d) => <CumulativeFlowChart data={d} height={CHART_H} />}
      </GadgetBody>
    </GadgetFrame>
  );
}

export function CreatedResolvedGadget() {
  const { gadget, scope, days, frame } = useScope();
  const q = useQuery(createdResolvedQuery(scope, days, gadget.config.interval ?? "day"));
  return (
    <GadgetFrame {...frame} dimmed={q.isPlaceholderData}>
      <GadgetBody query={q} empty={(d) => d.buckets.every((b) => b.created === 0 && b.resolved === 0 && b.open === 0) && nothing()}>
        {(d) => <CreatedResolvedChart data={d} height={CHART_H + 60} />}
      </GadgetBody>
    </GadgetFrame>
  );
}

export function CycleTimeGadget() {
  const { scope, days, frame, openIssue } = useScope();
  const q = useQuery(cycleTimeQuery(scope, days));
  return (
    <GadgetFrame {...frame} dimmed={q.isPlaceholderData}>
      <GadgetBody query={q} empty={(d) => d.issues.length === 0 && <GadgetMessage icon={<Timer className="size-6" />} title={`Nothing resolved in the last ${days} days`} body="Cycle time appears as issues are resolved." />}>
        {(d) => (
          <>
            <dl className="mb-3 flex flex-wrap gap-x-5 gap-y-1 text-[13px]">
              {[
                ["Median", formatDuration(d.medianHours)],
                ["85th percentile", formatDuration(d.p85Hours)],
                ["Resolved", String(d.issues.length)],
              ].map(([k, v]) => (
                <div key={k} className="flex items-baseline gap-1.5">
                  <dt className="text-muted">{k}</dt>
                  <dd className="font-semibold">{v}</dd>
                </div>
              ))}
            </dl>
            <CycleTimeChart report={d} onOpen={openIssue} height={CHART_H} />
          </>
        )}
      </GadgetBody>
    </GadgetFrame>
  );
}

export function BreakdownGadget() {
  const { gadget, scope, frame } = useScope();
  const c = gadget.config;
  const by = c.by ?? "assignee";
  const points = c.measure === "points";
  const q = useQuery(breakdownQuery(scope, by));
  const format = points ? formatPoints : (n: number) => String(n);
  return (
    <GadgetFrame {...frame} dimmed={q.isPlaceholderData}>
      <GadgetBody query={q} empty={(d) => (d.total === 0 ? nothing() : points && d.totalPoints === 0 ? nothing("None of the matching issues are estimated in story points.") : null)}>
        {(d) => {
          const rows: BreakdownRow[] = d.groups.map((g) => ({ key: g.key, label: g.label, value: points ? g.points : g.count, category: g.category, user: g.user }));
          const props = {
            rows,
            total: points ? d.totalPoints : d.total,
            unit: points ? "pts" : "issues",
            format,
            byCategory: by === "status" || by === "statusCategory",
            people: by === "assignee",
            jqlFor: (r: BreakdownRow) => scopeJql(c.projectKeys, c.jql, breakdownClause(by, r.key)),
            label: `${points ? "Points" : "Issues"} by ${breakdownLabel(by).toLowerCase()}`,
          };
          return c.chart === "donut" ? <BreakdownDonut {...props} byCategory={by === "statusCategory"} /> : <BreakdownBars {...props} />;
        }}
      </GadgetBody>
    </GadgetFrame>
  );
}

export function StatGadget() {
  const { gadget, scope, days, frame } = useScope();
  const stat = gadget.config.stat ?? "open";
  const q = useQuery(summaryQuery(scope, days));
  const jql = searchJql(gadget) ?? "";
  return (
    <GadgetFrame {...frame} dimmed={q.isPlaceholderData}>
      <GadgetBody query={q}>
        {(d) => {
          const v = statValue(d, stat);
          const alert = (stat === "overdue" || stat === "unassigned") && (v ?? 0) > 0;
          return (
            <div className="flex h-full flex-col justify-center">
              <Link
                to="/issues"
                search={{ jql: jql || undefined }}
                aria-label={`${formatStat(stat, v)} ${statMeta(stat).label.toLowerCase()}, ${statCaption(stat, days, d)}. Open in search`}
                className="self-start rounded-md text-5xl leading-none font-semibold tracking-tight hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
              >
                <span className={cn(alert && "text-danger")}>{formatStat(stat, v)}</span>
              </Link>
              <p className="mt-2 text-[13px] text-muted">{statCaption(stat, days, d)}</p>
            </div>
          );
        }}
      </GadgetBody>
    </GadgetFrame>
  );
}

export function IssueListGadget() {
  const { gadget, frame, openIssue } = useScope();
  const limit = gadget.config.limit ?? 10;
  const jql = scopeJql(gadget.config.projectKeys, gadget.config.jql) || "ORDER BY updated DESC";
  const q = useQuery(issueListQuery(jql, limit));
  const today = todayISO();
  return (
    <GadgetFrame {...frame} dimmed={q.isPlaceholderData}>
      <GadgetBody query={q} empty={(d) => d.issues.length === 0 && <GadgetMessage icon={<ListX className="size-6" />} title="No matching issues" body="Nothing matches this list's query right now." />}>
        {(d) => (
          <div className="-mx-4 -mt-4 -mb-4 flex h-[calc(100%+2rem)] flex-col">
            <table className="w-full table-fixed border-collapse text-[13px]">
              <thead className="sr-only @md:not-sr-only">
                <tr className="border-b border-line text-left text-xs text-muted [&>th]:px-2 [&>th]:py-2 [&>th]:font-semibold">
                  <th className="w-24 !pl-4">Key</th>
                  <th>Summary</th>
                  <th className="hidden w-32 @lg:table-cell">Status</th>
                  <th className="w-10">
                    <span className="sr-only">Assignee</span>
                  </th>
                  <th className="hidden w-8 @md:table-cell">
                    <span className="sr-only">Priority</span>
                  </th>
                  <th className="hidden w-20 !pr-4 @xl:table-cell">Due</th>
                </tr>
              </thead>
              <tbody>
                {d.issues.map((i) => {
                  const overdue = !!i.dueDate && i.dueDate < today && i.status.category !== "done";
                  return (
                    <tr
                      key={i.id}
                      onClick={() => openIssue(i.key)}
                      className="cursor-pointer border-b border-line last:border-b-0 hover:bg-surface-2 [&>td]:px-2 [&>td]:py-2"
                    >
                      <td className="!pl-4">
                        <a
                          href={`/browse/${i.key}`}
                          onClick={(e) => {
                            e.preventDefault();
                            e.stopPropagation();
                            openIssue(i.key);
                          }}
                          className="font-mono text-xs text-muted hover:text-accent focus-visible:outline-2 focus-visible:outline-accent"
                        >
                          {i.key}
                        </a>
                      </td>
                      <td className="truncate" title={i.summary}>
                        {i.summary}
                      </td>
                      <td className="hidden @lg:table-cell">
                        <StatusBadge status={i.status} />
                      </td>
                      <td>
                        <UserAvatar user={i.assignee ?? null} size={22} />
                      </td>
                      <td className="hidden @md:table-cell">
                        <PriorityIcon priority={i.priority} />
                      </td>
                      <td className={cn("hidden !pr-4 text-xs whitespace-nowrap @xl:table-cell", overdue ? "font-semibold text-danger" : "text-muted")}>
                        {i.dueDate ? formatDay(i.dueDate) : ""}
                        {overdue && <span className="sr-only"> (overdue)</span>}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            <p className="mt-auto border-t border-line px-4 py-2 text-xs text-muted">
              {d.total > d.issues.length ? `Showing ${d.issues.length} of ${d.total}` : `${d.total} issue${d.total === 1 ? "" : "s"}`}
            </p>
          </div>
        )}
      </GadgetBody>
    </GadgetFrame>
  );
}

export function UnknownGadget() {
  const { gadget } = useGadget();
  return (
    <GadgetFrame title={gadget.title || "Gadget"}>
      <GadgetMessage icon={<ChartNoAxesColumn className="size-6" />} title="This gadget isn't available" body="It may need a newer version of the app." />
    </GadgetFrame>
  );
}
