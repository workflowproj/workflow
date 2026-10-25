import { Fragment, useMemo, useState } from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { CalendarClock, ChevronLeft, ChevronRight, Download } from "lucide-react";
import { projectsQuery, useMe } from "@/api/queries";
import type { User } from "@/api/types";
import { Button, Card, EmptyState, IconButton, PageHeader, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { UserPicker } from "@/components/UserPicker";
import { cn } from "@/lib/cn";
import { formatDuration } from "@/lib/duration";
import { Segmented } from "@/features/reports/ChartBits";
import { localDate } from "@/features/board/card-fields";
import { timesheetQuery, useTimeUnits, type Timesheet } from "@/features/time/api";
import { hours, isWeekend, periodRange, shiftPeriod, timesheetCsv, type Period } from "@/features/time/model";

const route = getRouteApi("/app/timesheet");

const timeZone = (() => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
})();

const parseDay = (s: string) => {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
};
const weekday = new Intl.DateTimeFormat(undefined, { weekday: "short" });
const dayNum = new Intl.DateTimeFormat(undefined, { day: "numeric" });
const rangeFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" });
const monthFmt = new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" });

/**
 * TimesheetPage totals the time people logged per day, for a week or a month, across the
 * projects the viewer can browse (or one of them), optionally for one person. Each person's row
 * opens to show the issues; the whole sheet exports as CSV.
 */
export function TimesheetPage() {
  const search = route.useSearch();
  const navigate = useNavigate({ from: "/timesheet" });
  const { data: me } = useMe();
  const period: Period = search.period ?? "week";
  const { from, to } = periodRange(period, search.from ?? localDate(new Date()));
  const today = localDate(new Date());
  const { data: projects = [] } = useQuery(projectsQuery);
  const params = { from, to, projects: search.projects, user: search.user, tz: timeZone };
  const { data, isLoading, error, isPlaceholderData } = useQuery(timesheetQuery(params));
  const [picked, setPicked] = useState<User | null>(null);
  const units = useTimeUnits();

  const go = (next: Partial<typeof search>) => navigate({ search: (prev) => ({ ...prev, ...next }), replace: true });
  const userValue = search.user ? (picked?.id === search.user ? picked : (data?.people.find((p) => p.user?.id === search.user)?.user ?? (me?.id === search.user ? me : null))) : null;

  const label = period === "month" ? monthFmt.format(parseDay(from)) : `${rangeFmt.format(parseDay(from))} – ${rangeFmt.format(parseDay(to))}`;
  const exportCsv = () => {
    if (!data) return;
    const blob = new Blob([timesheetCsv(data)], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `timesheet-${from}-to-${to}${search.projects ? `-${search.projects}` : ""}.csv`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  };

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4 p-4 md:p-6">
      <PageHeader
        title="Timesheet"
        subtitle={`Time logged per person and day, in hours (${timeZone}).`}
        actions={
          <Button icon={<Download className="size-4" />} onClick={exportCsv} disabled={!data || data.people.length === 0}>
            Export CSV
          </Button>
        }
      />
      <div className="flex flex-wrap items-center gap-2">
        <Segmented
          label="Period"
          value={period}
          onChange={(p) => go({ period: p === "month" ? "month" : undefined, from: periodRange(p, from).from })}
          options={[
            { value: "week", label: "Week" },
            { value: "month", label: "Month" },
          ]}
        />
        <div className="flex items-center gap-1">
          <IconButton label={`Previous ${period}`} size="sm" onClick={() => go({ from: shiftPeriod(period, from, -1).from })}>
            <ChevronLeft className="size-4" />
          </IconButton>
          <span className="min-w-44 text-center text-sm font-medium tabular-nums" aria-live="polite">
            {label}
          </span>
          <IconButton label={`Next ${period}`} size="sm" onClick={() => go({ from: shiftPeriod(period, from, 1).from })}>
            <ChevronRight className="size-4" />
          </IconButton>
        </div>
        <Button size="sm" variant="ghost" disabled={today >= from && today <= to} onClick={() => go({ from: undefined })}>
          {period === "month" ? "This month" : "This week"}
        </Button>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <div className="w-60">
            <Select aria-label="Project" value={search.projects ?? ""} onChange={(e) => go({ projects: e.target.value || undefined })} className="h-8 text-[13px]">
              <option value="">All projects</option>
              {projects.map((p) => (
                <option key={p.key} value={p.key}>
                  {p.key} — {p.name}
                </option>
              ))}
            </Select>
          </div>
          <div className="w-52">
            <UserPicker
              label="Person"
              value={userValue}
              noneLabel="Everyone"
              onChange={(u) => {
                setPicked(u);
                go({ user: u?.id });
              }}
            />
          </div>
        </div>
      </div>
      {error && <ErrorBox error={error} />}
      <Card className={cn("transition-opacity", isPlaceholderData && "opacity-60")}>
        {isLoading || !data ? (
          !error && <PageSpinner label="Loading timesheet" />
        ) : data.people.length === 0 ? (
          <EmptyState icon={<CalendarClock className="size-8" />} title="No time logged" body={`Nobody logged work ${search.projects ? `on ${search.projects} ` : ""}between ${rangeFmt.format(parseDay(from))} and ${rangeFmt.format(parseDay(to))}. Log work from an issue's Time tracking panel.`} />
        ) : (
          <TimesheetTable data={data} today={today} totalLabel={formatDuration(data.total, units)} />
        )}
      </Card>
    </div>
  );
}

function TimesheetTable({ data, today, totalLabel }: { data: Timesheet; today: string; totalLabel: string }) {
  const [open, setOpen] = useState<Set<string>>(new Set());
  const toggle = (id: string) => setOpen((s) => (s.has(id) ? new Set([...s].filter((x) => x !== id)) : new Set([...s, id])));
  const people = useMemo(() => data.people.map((p, k) => ({ ...p, rowId: p.user?.id ?? `former-${k}` })), [data.people]);
  const cell = "px-2 py-2 text-right tabular-nums text-[13px] whitespace-nowrap";
  const dayCls = (d: string) => cn(isWeekend(d) && "bg-surface-2/60", d === today && "bg-accent-soft/60");
  return (
    <div className="overflow-x-auto">
      <p className="px-4 pt-3 text-[13px] text-muted">
        {totalLabel} logged by {people.length} {people.length === 1 ? "person" : "people"}. Select a person to see their issues.
      </p>
      <table className="mt-2 w-full border-collapse text-sm" aria-label="Timesheet">
        <thead>
          <tr className="border-b border-line text-xs text-muted">
            <th scope="col" className="sticky left-0 z-10 min-w-56 bg-surface px-4 py-2 text-left font-semibold">
              Person
            </th>
            {data.dates.map((d) => (
              <th key={d} scope="col" className={cn("min-w-11 px-2 py-1.5 text-right font-medium", dayCls(d))}>
                <span className="block text-[11px] text-faint">{weekday.format(parseDay(d))}</span>
                <span className={cn(d === today && "font-semibold text-accent")}>{dayNum.format(parseDay(d))}</span>
              </th>
            ))}
            <th scope="col" className="min-w-16 px-4 py-2 text-right font-semibold">
              Total
            </th>
          </tr>
        </thead>
        <tbody>
          {people.map((p) => {
            const expanded = open.has(p.rowId);
            return (
              <Fragment key={p.rowId}>
                <tr className="border-b border-line hover:bg-surface-2/40">
                  <th scope="row" className="sticky left-0 z-10 bg-surface px-2 py-1.5 text-left font-normal">
                    <button
                      type="button"
                      aria-expanded={expanded}
                      onClick={() => toggle(p.rowId)}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent"
                    >
                      <ChevronRight className={cn("size-3.5 shrink-0 text-faint transition-transform", expanded && "rotate-90")} />
                      <UserAvatar user={p.user ?? { displayName: "Former user", kind: "human" }} size={22} />
                      <span className="truncate font-medium">{p.user?.displayName ?? "Former user"}</span>
                    </button>
                  </th>
                  {p.days.map((s, k) => (
                    <td key={data.dates[k]} className={cn(cell, dayCls(data.dates[k]), !s && "text-faint")}>
                      {hours(s) || "·"}
                    </td>
                  ))}
                  <td className={cn(cell, "px-4 font-semibold")}>{hours(p.total)}</td>
                </tr>
                {expanded &&
                  p.issues.map((i) => (
                    <tr key={i.issue.id} className="border-b border-line bg-surface-2/30 text-muted">
                      <th scope="row" className="sticky left-0 z-10 bg-surface py-1.5 pr-2 pl-12 text-left font-normal">
                        <span className="flex min-w-0 items-center gap-2">
                          <IssueTypeIcon type={i.issue.type} />
                          <Link to="/browse/$issueKey" params={{ issueKey: i.issue.key }} className="shrink-0 font-mono text-xs font-medium text-ink hover:underline">
                            {i.issue.key}
                          </Link>
                          <span className="max-w-64 truncate text-[13px]" title={i.issue.summary}>
                            {i.issue.summary}
                          </span>
                        </span>
                      </th>
                      {i.days.map((s, k) => (
                        <td key={data.dates[k]} className={cn(cell, dayCls(data.dates[k]), !s && "text-faint")}>
                          {hours(s)}
                        </td>
                      ))}
                      <td className={cn(cell, "px-4")}>{hours(i.total)}</td>
                    </tr>
                  ))}
              </Fragment>
            );
          })}
        </tbody>
        <tfoot>
          <tr className="text-ink">
            <th scope="row" className="sticky left-0 z-10 bg-surface px-4 py-2.5 text-left font-semibold">
              Total
            </th>
            {data.dayTotals.map((s, k) => (
              <td key={data.dates[k]} className={cn(cell, "font-semibold", dayCls(data.dates[k]), !s && "text-faint")}>
                {hours(s) || "·"}
              </td>
            ))}
            <td className={cn(cell, "px-4 font-semibold")}>{hours(data.total)}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  );
}
