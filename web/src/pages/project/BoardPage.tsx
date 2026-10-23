import { useCallback, useMemo, useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { CalendarClock, Columns3, ListTodo, TriangleAlert } from "lucide-react";
import { hasPermission, statusesQuery, useMe, useProject } from "@/api/queries";
import type { Project } from "@/api/types";
import { Button, EmptyState, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { BoardSwitcher } from "@/features/board/BoardSwitcher";
import { BoardView } from "@/features/board/BoardView";
import { hasFilters, NO_FILTERS, quickFilterJql, type QuickFilters } from "@/features/board/jql";
import { SWIMLANE_LABELS, type SwimlaneMode } from "@/features/board/model";
import { QuickFilterBar } from "@/features/board/QuickFilterBar";
import { boardDataQuery, useBoardSelection, type Board, type BoardData } from "@/features/board/queries";
import { primaryLink } from "@/features/board/styles";
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

  const config = data?.board ?? board;
  const mode = groupBy ?? config.swimlanes;
  const scrum = config.type === "scrum";
  const sprint = data?.sprint;
  const canDrag = ["issue.transition", "issue.edit", "sprint.manage"].some((p) => hasPermission(project, p));
  const total = data?.columns.reduce((n, c) => n + c.issues.length, 0) ?? 0;

  return (
    <>
      <div className="flex shrink-0 flex-col gap-3 px-4 pt-4 pb-3 md:px-6">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <BoardSwitcher boards={boards} board={board} onSelect={onSelect} />
          {scrum && sprint && <SprintSummary sprint={sprint} />}
          {scrum && sprint && hasPermission(project, "sprint.manage") && (
            <Button size="sm" className="ml-auto" onClick={() => setCompleting(true)}>
              Complete sprint
            </Button>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <QuickFilterBar value={filters} onChange={setFilters} />
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
        {data && data.unmappedStatusIds.length > 0 && <UnmappedNotice project={project} names={data.unmappedStatusIds.map((id) => statusMap.get(id)?.name ?? "Unknown")} />}
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
          <BoardView columns={data.columns} mode={mode} meId={me?.id} statuses={statusMap} canDrag={canDrag} onOpen={onOpen} onDrop={move.drop} />
        )}
      </div>
      {move.dialogs}
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

function UnmappedNotice({ names, project }: { names: string[]; project: Project }) {
  const list = names.length > 1 ? `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}` : names[0];
  return (
    <div className="flex items-center gap-2 rounded-md border border-warning/30 bg-warning-soft px-3 py-2 text-[13px] text-ink">
      <TriangleAlert className="size-4 shrink-0 text-warning" />
      <span className="flex-1">
        Issues in <strong className="font-medium">{list}</strong> aren't shown: {names.length > 1 ? "those statuses aren't" : "that status isn't"} mapped to a column.
      </span>
      {hasPermission(project, "board.manage") && (
        <Link to="/projects/$projectKey/settings/boards" params={{ projectKey: project.key }} className="shrink-0 font-medium text-accent hover:underline">
          Configure board
        </Link>
      )}
    </div>
  );
}
