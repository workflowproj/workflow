import { useCallback, useMemo, useRef, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { CalendarClock, Columns3, ListTodo, Settings2, TriangleAlert } from "lucide-react";
import { hasPermission, statusesQuery, useMe, useProject } from "@/api/queries";
import type { Project, Status, User } from "@/api/types";
import { Button, EmptyState, PageSpinner, Popover, Select } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { BoardSwitcher } from "@/features/board/BoardSwitcher";
import { joinNames } from "@/features/board/board-config";
import { BoardView } from "@/features/board/BoardView";
import { normalizeCardFields } from "@/features/board/card-fields";
import { ConfigureBoard, type ConfigureTab } from "@/features/board/ConfigureBoard";
import { hasFilters, NO_FILTERS, quickFilterJql, type QuickFilters } from "@/features/board/jql";
import { SWIMLANE_LABELS, type SwimlaneMode } from "@/features/board/model";
import { filterColumns, peopleOf } from "@/features/board/people";
import { QuickFilterBar } from "@/features/board/QuickFilterBar";
import { boardDataQuery, useBoardSelection, type Board, type BoardData } from "@/features/board/queries";
import { primaryLink } from "@/features/board/styles";
import { useColumnTools } from "@/features/board/useBoardConfig";
import { useBoardMove } from "@/features/board/useBoardMove";
import { formatRange, remainingLabel } from "@/features/backlog/dates";
import { CompleteSprintDialog } from "@/features/backlog/SprintDialogs";
import { cn } from "@/lib/cn";

const route = getRouteApi("/app/projects/$projectKey/board");

export function BoardPage() {
  const { projectKey } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const { data: project } = useProject(projectKey);
  const { boards, board, select, isLoading, error } = useBoardSelection(projectKey);
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);

  let body;
  if (isLoading || !project) body = <PageSpinner label="Loading board" />;
  else if (error) body = <ErrorBox error={error} className="m-6" />;
  else if (!board)
    body = (
      <EmptyState
        icon={<Columns3 className="size-8" />}
        title="This project has no board yet"
        body="Boards show issues in columns mapped to workflow statuses."
        action={
          hasPermission(project, "board.manage") && (
            <Link to="/projects/$projectKey/settings/boards" params={{ projectKey }} className={primaryLink}>
              Create a board
            </Link>
          )
        }
      />
    );
  else body = <BoardScreen key={board.id} project={project} boards={boards} board={board} onSelect={select} onOpen={openIssue} />;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {body}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

function BoardScreen({ project, boards, board, onSelect, onOpen }: { project: Project; boards: Board[]; board: Board; onSelect: (id: string) => void; onOpen: (key: string) => void }) {
  const [filters, setFilters] = useState<QuickFilters>(NO_FILTERS);
  const jql = quickFilterJql(filters);
  const { data, isLoading, error, isPlaceholderData } = useQuery(boardDataQuery(board.id, jql));
  const [groupBy, setGroupBy] = useState<SwimlaneMode | null>(null);
  const { data: me } = useMe();
  const { data: statuses = [] } = useQuery(statusesQuery);
  const statusMap = useMemo(() => new Map(statuses.map((s) => [s.id, s])), [statuses]);
  const move = useBoardMove(board.id, jql);
  const [completing, setCompleting] = useState(false);
  const [configuring, setConfiguring] = useState<ConfigureTab | null>(null);

  const config = data?.board ?? board;
  const mode = groupBy ?? config.swimlanes;
  const scrum = config.type === "scrum";
  const sprint = data?.sprint;
  const canDrag = ["issue.transition", "issue.edit", "sprint.manage"].some((p) => hasPermission(project, p));
  const canManage = hasPermission(project, "board.manage");
  const cardFields = useMemo(() => normalizeCardFields(config.cardFields), [config.cardFields]);
  const { tools, mapStatus } = useColumnTools({ board: config, statuses: statusMap, onConfigure: () => setConfiguring("columns") });
  const issueCounts = useMemo(() => {
    const m = new Map<string, number>();
    for (const c of data?.columns ?? []) for (const i of c.issues) m.set(i.status.id, (m.get(i.status.id) ?? 0) + 1);
    return m;
  }, [data]);
  // The people filter applies here, so toggling an avatar is instant and the avatars come from
  // every issue the other filters leave on the board.
  const known = useRef(new Map<string, User>());
  const people = useMemo(() => {
    const all = data?.columns.flatMap((c) => c.issues) ?? [];
    for (const i of all) if (i.assignee) known.current.set(i.assignee.id, i.assignee);
    return peopleOf(all, me?.id, filters.people, known.current);
  }, [data, me?.id, filters.people]);
  const columns = useMemo(() => filterColumns(data?.columns ?? [], filters.people), [data, filters.people]);
  const total = columns.reduce((n, c) => n + c.issues.length, 0);
  const noSprint = scrum && !!data && !sprint && !isPlaceholderData;

  return (
    <>
      <div className="flex shrink-0 flex-col gap-3 px-4 pt-4 pb-3 md:px-6">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <BoardSwitcher boards={boards} board={board} onSelect={onSelect} />
          {scrum && sprint && <SprintSummary sprint={sprint} />}
          <div className="ml-auto flex items-center gap-2">
            {canManage && (
              <Button size="sm" variant="ghost" icon={<Settings2 className="size-3.5" />} onClick={() => setConfiguring("columns")}>
                Configure board
              </Button>
            )}
            {scrum && sprint && hasPermission(project, "sprint.manage") && (
              <Button size="sm" onClick={() => setCompleting(true)}>
                Complete sprint
              </Button>
            )}
          </div>
        </div>
        <div className={cn("flex flex-wrap items-center justify-between gap-2", noSprint && "hidden")}>
          <QuickFilterBar value={filters} onChange={setFilters} people={people} meId={me?.id} />
          <label className="flex items-center gap-2 text-[13px] whitespace-nowrap text-muted">
            Group by
            <span className="w-36">
              <Select value={mode} onChange={(e) => setGroupBy(e.target.value as SwimlaneMode)} className="h-8 text-[13px]">
                {(Object.keys(SWIMLANE_LABELS) as SwimlaneMode[]).map((m) => (
                  <option key={m} value={m}>
                    {m === "none" ? "None" : SWIMLANE_LABELS[m]}
                  </option>
                ))}
              </Select>
            </span>
          </label>
        </div>
        {data && data.unmappedStatusIds.length > 0 && (
          <UnmappedNotice
            statuses={data.unmappedStatusIds.map((id) => statusMap.get(id) ?? { id, name: "Unknown", category: "todo" })}
            columns={config.columns}
            canManage={canManage}
            onMap={mapStatus}
            onConfigure={() => setConfiguring("columns")}
          />
        )}
        {data && hasFilters(filters) && total === 0 && (!scrum || sprint) && (
          <p className="text-sm text-muted">
            No issues match these filters.{" "}
            <button type="button" className="font-medium text-accent hover:underline" onClick={() => setFilters(NO_FILTERS)}>
              Clear filters
            </button>
          </p>
        )}
      </div>
      <div className={cn("min-h-0 flex-1 overflow-auto transition-opacity", isPlaceholderData && "opacity-60")}>
        {isLoading ? (
          <PageSpinner label="Loading board" />
        ) : error ? (
          <ErrorBox error={error} className="mx-6" />
        ) : !data ? null : scrum && !sprint ? (
          <NoActiveSprint projectKey={project.key} />
        ) : (
          <BoardView
            columns={columns}
            mode={mode}
            meId={me?.id}
            statuses={statusMap}
            cardFields={cardFields}
            canDrag={canDrag}
            columnTools={canManage ? tools : undefined}
            onOpen={onOpen}
            onDrop={move.drop}
          />
        )}
      </div>
      {move.dialogs}
      {configuring && <ConfigureBoard key={config.id} board={config} issueCounts={issueCounts} initialTab={configuring} onClose={() => setConfiguring(null)} />}
      <CompleteSprintDialog open={completing} onClose={() => setCompleting(false)} sprint={sprint ?? null} projectKey={project.key} />
    </>
  );
}

function SprintSummary({ sprint }: { sprint: NonNullable<BoardData["sprint"]> }) {
  const left = remainingLabel(sprint.endDate);
  return (
    <div className="flex min-w-0 items-center gap-3 border-l border-line pl-3 text-sm">
      <div className="min-w-0">
        <p className="truncate font-medium">{sprint.name}</p>
        {sprint.goal && <p className="max-w-xl truncate text-xs text-muted" title={sprint.goal}>{sprint.goal}</p>}
      </div>
      {(sprint.startDate || left) && (
        <span className={cn("inline-flex shrink-0 items-center gap-1.5 text-xs", left?.overdue ? "text-danger" : "text-muted")} title={formatRange(sprint.startDate, sprint.endDate)}>
          <CalendarClock className="size-3.5" />
          {left?.text ?? formatRange(sprint.startDate, sprint.endDate)}
        </span>
      )}
    </div>
  );
}

function NoActiveSprint({ projectKey }: { projectKey: string }) {
  return (
    <div className="mx-4 rounded-lg border border-dashed border-line-strong bg-surface md:mx-6">
      <EmptyState
        icon={<ListTodo className="size-8" />}
        title="No active sprint"
        body="This board shows the active sprint. Plan the next sprint in the backlog and start it to see its issues here."
        action={
          <Link to="/projects/$projectKey/backlog" params={{ projectKey }} className={primaryLink}>
            Go to backlog
          </Link>
        }
      />
    </div>
  );
}

/** UnmappedNotice warns that some issues are hidden and lets board managers map their statuses on the spot. */
function UnmappedNotice({
  statuses,
  columns,
  canManage,
  onMap,
  onConfigure,
}: {
  statuses: Status[];
  columns: { id?: string; name: string }[];
  canManage: boolean;
  onMap: (statusId: string, columnId: string | "new") => void;
  onConfigure: () => void;
}) {
  const several = statuses.length > 1;
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-warning/30 bg-warning-soft px-3 py-2 text-[13px] text-ink">
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <TriangleAlert className="size-4 shrink-0 text-warning" />
        <span>
          Issues in <strong className="font-medium">{joinNames(statuses.map((s) => s.name))}</strong> aren't shown: {several ? "those statuses aren't" : "that status isn't"} mapped to a column.
        </span>
      </span>
      {canManage && (
        <span className="flex shrink-0 items-center gap-3">
          <Popover
            align="end"
            trigger={({ toggle, open }) => (
              <Button size="sm" onClick={toggle} aria-expanded={open} aria-haspopup="dialog">
                Map {several ? "statuses" : "status"}
              </Button>
            )}
            content={(close) => (
              <div role="dialog" aria-label="Map statuses to columns" className="flex w-72 flex-col gap-3 p-2">
                <p className="text-xs text-muted">Pick a column for each status; its issues show there straight away.</p>
                {statuses.map((s) => (
                  <div key={s.id} className="flex flex-col gap-1">
                    <StatusBadge status={s} className="self-start" />
                    <Select
                      aria-label={`Column for ${s.name}`}
                      value=""
                      onChange={(e) => {
                        if (!e.target.value) return;
                        onMap(s.id, e.target.value);
                        if (statuses.length === 1) close();
                      }}
                      className="h-8 text-[13px]"
                    >
                      <option value="">Choose a column…</option>
                      {columns.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name}
                        </option>
                      ))}
                      <option value="new">New column “{s.name}”</option>
                    </Select>
                  </div>
                ))}
              </div>
            )}
          />
          <button type="button" className="font-medium text-accent hover:underline" onClick={onConfigure}>
            Configure board
          </button>
        </span>
      )}
    </div>
  );
}
