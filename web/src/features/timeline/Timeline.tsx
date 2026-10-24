import { useCallback, useMemo, useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { CalendarRange, Info, TriangleAlert } from "lucide-react";
import { projectsQuery } from "@/api/queries";
import type { Issue } from "@/api/types";
import { EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { buildRows, dependencies, isOverdue, schedules, stats, todayDay } from "./model";
import { timelineQuery, useEpicProgress, useTimelineEdits } from "./queries";
import type { Zoom } from "./scale";
import { TimelineGrid } from "./TimelineGrid";
import { Chip, ProjectEvents, Stats, ZoomSwitch, type QuickFilters } from "./Toolbar";

/** Live updates come from one event stream per project; beyond this many, the view polls instead. */
const MAX_STREAMS = 4;

const ZOOM_KEY = "wfm.timeline.zoom";
function readZoom(): Zoom {
  try {
    const z = localStorage.getItem(ZOOM_KEY);
    return z === "weeks" || z === "quarters" ? z : "months";
  } catch {
    return "months";
  }
}

/**
 * Timeline is the roadmap view shared by a project's Timeline tab and the cross-project big
 * picture: a tree of epics and issues beside a time grid with bars, sprints, releases and
 * dependencies. `projects` is a comma list ("" = all browsable projects).
 */
export function Timeline({
  projects,
  multi,
  baseJql,
  onOpen,
  picker,
  live,
}: {
  projects: string;
  multi: boolean;
  baseJql?: string;
  onOpen: (key: string) => void;
  picker?: ReactNode;
  /** Open live event streams for the projects shown (the project page already has one). */
  live?: boolean;
}) {
  const [zoom, setZoomState] = useState<Zoom>(readZoom);
  const setZoom = (z: Zoom) => {
    setZoomState(z);
    try {
      localStorage.setItem(ZOOM_KEY, z);
    } catch {
      // The choice then lasts for this visit only.
    }
  };
  const [showDone, setShowDone] = useState(true);
  const [filters, setFilters] = useState<QuickFilters>({ overdue: false, conflicts: false, mine: false });
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [todaySignal, setTodaySignal] = useState(0);
  const today = todayDay();

  const jql = [baseJql, filters.mine ? "assignee = currentUser()" : ""].filter(Boolean).map((j) => `(${j})`).join(" AND ");
  const query = timelineQuery(projects, jql, showDone ? 90 : 0);
  const keys = projects ? projects.split(",") : [];
  const polling = live && keys.length !== 1 && (keys.length === 0 || keys.length > MAX_STREAMS);
  const { data, isLoading, error, isPlaceholderData } = useQuery({ ...query, refetchInterval: polling ? 30_000 : false });
  const { data: allProjects = [] } = useQuery(projectsQuery);
  const editable = useMemo(() => new Set(allProjects.filter((p) => p.myPermissions.includes("issue.edit")).map((p) => p.key)), [allProjects]);
  const canEdit = useCallback((key: string) => editable.has(key), [editable]);
  const edits = useTimelineEdits(query.queryKey);

  const sched = useMemo(() => (data ? schedules(data) : new Map()), [data]);
  const { deps, warnings } = useMemo(() => (data ? dependencies(data, sched) : { deps: [], warnings: new Map<string, string[]>() }), [data, sched]);
  const counts = useMemo(() => (data ? stats(data, sched, deps, today) : { inProgress: 0, overdue: 0, conflicts: 0, unscheduled: 0 }), [data, sched, deps, today]);
  const include = useMemo(
    () => (filters.overdue || filters.conflicts ? (i: Issue) => (!filters.overdue || isOverdue(i, today)) && (!filters.conflicts || warnings.has(i.key)) : undefined),
    [filters.overdue, filters.conflicts, warnings, today],
  );
  const rows = useMemo(() => (data ? buildRows(data, { multi, collapsed, include }) : []), [data, multi, collapsed, include]);
  const epicKeys = useMemo(() => (data?.issues ?? []).filter((i) => i.type.level >= 1).map((i) => i.key), [data]);
  const { data: progress } = useEpicProgress(epicKeys);

  const toggle = (id: string) => setCollapsed((s) => (s.has(id) ? new Set([...s].filter((x) => x !== id)) : new Set(s).add(id)));
  const toggleAll = (collapse: boolean) => {
    if (!collapse) return setCollapsed(new Set());
    const all = buildRows(data ?? { projects: [], issues: [] }, { multi, collapsed: new Set(), include });
    setCollapsed(new Set(all.filter((r) => r.kind !== "issue" || r.childCount > 0).map((r) => r.id)));
  };
  const flip = (k: keyof QuickFilters) => setFilters((f) => ({ ...f, [k]: !f[k] }));
  const single = !multi && data?.projects.length === 1 ? data.projects[0] : undefined;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {live && data?.projects.length && data.projects.length <= MAX_STREAMS ? data.projects.map((p) => <ProjectEvents key={p.key} projectKey={p.key} />) : null}
      <div className="flex shrink-0 flex-col gap-2.5 px-4 pt-4 pb-3 md:px-6">
        <div className="flex flex-wrap items-center gap-2">
          {picker}
          <Chip pressed={filters.overdue} onClick={() => flip("overdue")} title="Show only issues past their due date">
            Only overdue
          </Chip>
          <Chip pressed={filters.conflicts} onClick={() => flip("conflicts")} title="Show only issues with a dependency conflict">
            Only conflicts
          </Chip>
          <Chip pressed={filters.mine} onClick={() => flip("mine")} title="assignee = currentUser()">
            Assigned to me
          </Chip>
          <Chip pressed={showDone} onClick={() => setShowDone((v) => !v)} title="Include issues resolved in the last 90 days">
            Show done
          </Chip>
          <div className="ml-auto">
            <ZoomSwitch zoom={zoom} onChange={setZoom} onToday={() => setTodaySignal((n) => n + 1)} />
          </div>
        </div>
        {data && <Stats counts={counts} />}
        {data?.truncated && (
          <Notice tone="warning">The timeline shows the first 2,000 issues only. Narrow it with the project picker or a filter.</Notice>
        )}
        {data && data.issues.length > 0 && sched.size === 0 && (
          <Notice tone="info">
            Nothing is scheduled yet. Hover an issue's row and drag across the grid to set its start and due dates, or set them on the issue. Epics can follow their stories
            automatically: add the automation template <strong className="font-medium">“Keep epic dates covering their stories”</strong> in{" "}
            {single ? (
              <Link to="/projects/$projectKey/settings/automation" params={{ projectKey: single.key }} className="font-medium text-accent hover:underline">
                project settings → Automation
              </Link>
            ) : (
              "each project's settings → Automation"
            )}
            .
          </Notice>
        )}
      </div>
      <div className={cn("flex min-h-0 flex-1 flex-col border-t border-line bg-surface transition-opacity", isPlaceholderData && "opacity-70")}>
        {isLoading ? (
          <PageSpinner label="Loading timeline" />
        ) : error ? (
          <ErrorBox error={error} className="m-4" />
        ) : !data ? null : rows.length === 0 ? (
          <EmptyState
            icon={<CalendarRange className="size-8" />}
            title={data.issues.length === 0 ? (jql ? "No issues match these filters" : "No issues to plan yet") : filters.conflicts ? "No dependency conflicts" : "Nothing is overdue"}
            body={
              data.issues.length === 0
                ? "Epics and their issues appear here with their dates. Create an epic and a few stories to start planning."
                : "Turn the quick filters off to see the whole plan."
            }
          />
        ) : (
          <TimelineGrid
            data={data}
            rows={rows}
            sched={sched}
            deps={deps}
            warnings={warnings}
            progress={progress}
            zoom={zoom}
            multi={multi}
            collapsed={collapsed}
            onToggle={toggle}
            onToggleAll={toggleAll}
            canEdit={canEdit}
            onOpen={onOpen}
            edits={edits}
            todaySignal={todaySignal}
            today={today}
          />
        )}
      </div>
    </div>
  );
}

function Notice({ tone, children }: { tone: "info" | "warning"; children: ReactNode }) {
  const Icon = tone === "warning" ? TriangleAlert : Info;
  return (
    <div className={cn("flex items-start gap-2 rounded-md border px-3 py-2 text-[13px]", tone === "warning" ? "border-warning/30 bg-warning-soft" : "border-accent/25 bg-accent-soft/60")}>
      <Icon className={cn("mt-0.5 size-4 shrink-0", tone === "warning" ? "text-warning" : "text-accent")} />
      <p className="text-ink">{children}</p>
    </div>
  );
}
