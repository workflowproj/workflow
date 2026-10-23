import { useCallback, useMemo, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { DndContext, DragOverlay, type Announcements, type UniqueIdentifier } from "@dnd-kit/core";
import { ArrowDownToLine, ArrowUpToLine, CalendarRange, ListTodo, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { hasPermission, useProject } from "@/api/queries";
import type { Issue, Project } from "@/api/types";
import { Badge, Button, EmptyState, IconButton, MenuItem, MenuLabel, PageSpinner, Popover, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { BoardSwitcher } from "@/features/board/BoardSwitcher";
import { hasFilters, NO_FILTERS, quickFilterJql, type QuickFilters } from "@/features/board/jql";
import { BACKLOG, findContainer, sumPoints, type Containers } from "@/features/board/model";
import { QuickFilterBar } from "@/features/board/QuickFilterBar";
import { backlogQuery, useBoardSelection, type Board, type Sprint } from "@/features/board/queries";
import { useContainerDnd } from "@/features/board/useContainerDnd";
import { BacklogSection } from "@/features/backlog/BacklogSection";
import { formatRange, remainingLabel } from "@/features/backlog/dates";
import { InlineCreate } from "@/features/backlog/InlineCreate";
import { IssueRow } from "@/features/backlog/IssueRow";
import { CompleteSprintDialog, EditSprintDialog, StartSprintDialog } from "@/features/backlog/SprintDialogs";
import { useBacklogMove } from "@/features/backlog/useBacklogMove";

const route = getRouteApi("/app/projects/$projectKey/backlog");

export function BacklogPage() {
  const { projectKey } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const { data: project } = useProject(projectKey);
  const { boards, board, select, isLoading, error } = useBoardSelection(projectKey);
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);

  let body;
  if (isLoading || !project) body = <PageSpinner label="Loading backlog" />;
  else if (error) body = <ErrorBox error={error} />;
  else if (!board) body = <EmptyState icon={<ListTodo className="size-8" />} title="This project has no board yet" body="The backlog belongs to a board. Ask a project admin to create one." />;
  else body = <BacklogScreen key={board.id} project={project} boards={boards} board={board} onSelect={select} onOpen={openIssue} />;

  return (
    <div className="mx-auto max-w-6xl p-4 md:p-6">
      {body}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

type Dialog = { kind: "edit" | "start" | "complete"; sprint: Sprint } | null;

function BacklogScreen({ project, boards, board, onSelect, onOpen }: { project: Project; boards: Board[]; board: Board; onSelect: (id: string) => void; onOpen: (key: string) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [filters, setFilters] = useState<QuickFilters>(NO_FILTERS);
  const jql = quickFilterJql(filters);
  const { data, isLoading, error, isPlaceholderData } = useQuery(backlogQuery(board.id, jql));
  const move = useBacklogMove(board.id, jql);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<Dialog>(null);
  const scrum = board.type === "scrum";
  const manage = hasPermission(project, "sprint.manage");
  const canCreate = hasPermission(project, "issue.create");
  const canDrag = manage || hasPermission(project, "issue.edit");

  const issues = useMemo(() => {
    const m = new Map<string, Issue>();
    for (const i of data?.backlog ?? []) m.set(i.id, i);
    for (const s of data?.sprints ?? []) for (const i of s.issues) m.set(i.id, i);
    return m;
  }, [data]);
  const containers = useMemo(() => {
    const c: Containers = {};
    for (const s of data?.sprints ?? []) c[s.sprint.id] = s.issues.map((i) => i.id);
    c[BACKLOG] = (data?.backlog ?? []).map((i) => i.id);
    return c;
  }, [data]);
  const dnd = useContainerDnd({ containers, onDrop: ({ id, to, list }) => move.drop(id, to, list) });
  const active = dnd.activeId ? issues.get(dnd.activeId) : undefined;
  const target = dnd.activeId ? findContainer(dnd.items, dnd.activeId) : undefined;

  const sprintName = (id: string) => (id === BACKLOG ? "Backlog" : (data?.sprints.find((s) => s.sprint.id === id)?.sprint.name ?? "sprint"));
  const where = (id: UniqueIdentifier) => sprintName(findContainer(dnd.items, String(id)) ?? "");
  const keyOf = (id: UniqueIdentifier) => issues.get(String(id))?.key ?? "issue";
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${keyOf(active.id)} in ${where(active.id)}.`,
    onDragOver: ({ active, over }) => (over ? `${keyOf(active.id)} is over ${where(over.id)}.` : `${keyOf(active.id)} is not over a list.`),
    onDragEnd: ({ active, over }) => (over ? `${keyOf(active.id)} was dropped in ${where(over.id)}.` : `${keyOf(active.id)} was dropped.`),
    onDragCancel: ({ active }) => `Moving ${keyOf(active.id)} was cancelled.`,
  };

  const createSprint = useMutation({
    mutationFn: () => unwrap(api.POST("/projects/{projectKey}/sprints", { params: { path: { projectKey: project.key } }, body: {} })),
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ["backlog", board.id] });
      qc.invalidateQueries({ queryKey: ["sprints", project.key] });
      toast.success(`${s.name} created`);
    },
    onError: (e) => toast.error(e),
  });
  const deleteSprint = async (s: Sprint, count: number) => {
    const ok = await confirm({
      title: `Delete ${s.name}?`,
      body: count ? `Its ${count} issue${count === 1 ? "" : "s"} will move to the backlog.` : "The sprint has no issues.",
      confirmLabel: "Delete sprint",
      danger: true,
    });
    if (!ok) return;
    try {
      await unwrap(api.DELETE("/sprints/{sprintId}", { params: { path: { sprintId: s.id } } }));
      qc.invalidateQueries({ queryKey: ["backlog", board.id] });
      qc.invalidateQueries({ queryKey: ["sprints", project.key] });
      toast.success(`${s.name} deleted`);
    } catch (e) {
      toast.error(e);
    }
  };

  const toggle = (id: string) => setCollapsed((s) => (s.has(id) ? new Set([...s].filter((x) => x !== id)) : new Set(s).add(id)));
  const hasActive = !!data?.sprints.some((s) => s.sprint.state === "active");
  const lists = [...(data?.sprints ?? []).map((s) => s.sprint), null];

  // Menu alternative to drag and drop: send an issue to the top or bottom of any list.
  const rowActions = (issue: Issue) =>
    canDrag && (
      <Popover
        align="end"
        trigger={({ toggle }) => (
          <IconButton label={`Actions for ${issue.key}`} size="sm" className="size-6 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 aria-expanded:opacity-100" onClick={toggle}>
            <MoreHorizontal className="size-4" />
          </IconButton>
        )}
        content={(close) => (
          <div role="menu" className="w-56">
            {lists.map((s) => {
              const id = s?.id ?? BACKLOG;
              const here = (issue.sprintId ?? BACKLOG) === id;
              return (
                <div key={id}>
                  <MenuLabel>{s?.name ?? "Backlog"}</MenuLabel>
                  <MenuItem icon={<ArrowUpToLine className="size-4" />} onClick={() => (close(), move.moveTo(issue.id, id, "top"))}>
                    {here ? "Move to top" : "Add to top"}
                  </MenuItem>
                  <MenuItem icon={<ArrowDownToLine className="size-4" />} onClick={() => (close(), move.moveTo(issue.id, id, "bottom"))}>
                    {here ? "Move to bottom" : "Add to bottom"}
                  </MenuItem>
                </div>
              );
            })}
          </div>
        )}
      />
    );

  if (isLoading) return <PageSpinner label="Loading backlog" />;
  if (error) return <ErrorBox error={error} />;
  if (!data) return null;
  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <BoardSwitcher boards={boards} board={board} onSelect={onSelect} />
        {scrum && manage && (
          <Button className="ml-auto" size="sm" icon={<Plus className="size-3.5" />} loading={createSprint.isPending} onClick={() => createSprint.mutate()}>
            Create sprint
          </Button>
        )}
      </div>
      <div className="mb-4">
        <QuickFilterBar value={filters} onChange={setFilters} />
      </div>
      {!scrum && <p className="mb-4 text-[13px] text-muted">Kanban boards don't use sprints: this is every open issue in rank order. Drag issues to re-prioritise them.</p>}
      <DndContext
        {...dnd.contextProps}
        accessibility={{ announcements, screenReaderInstructions: { draggable: "To pick up an issue, press Space. Use the arrow keys to move it within or between sprints, Space to drop it and Escape to cancel. Press Enter to open it." } }}
      >
        <div className={`flex flex-col gap-4 transition-opacity ${isPlaceholderData ? "opacity-60" : ""}`}>
          {data.sprints.map(({ sprint, issues: list }) => {
            const isActive = sprint.state === "active";
            const left = isActive ? remainingLabel(sprint.endDate) : null;
            return (
              <BacklogSection
                key={sprint.id}
                id={sprint.id}
                title={sprint.name}
                badge={isActive ? <Badge tone="accent">Active</Badge> : undefined}
                meta={
                  (sprint.startDate || sprint.endDate) && (
                    <span className="inline-flex items-center gap-1">
                      <CalendarRange className="size-3.5" />
                      {formatRange(sprint.startDate, sprint.endDate)}
                      {left && <span className={left.overdue ? "text-danger" : ""}> · {left.text}</span>}
                    </span>
                  )
                }
                description={sprint.goal || undefined}
                issues={issues}
                items={dnd.items[sprint.id] ?? []}
                collapsed={collapsed.has(sprint.id)}
                onToggle={() => toggle(sprint.id)}
                onOpen={onOpen}
                canDrag={canDrag}
                highlight={!!active && target === sprint.id}
                rowActions={rowActions}
                empty={isActive ? "Drag issues here to add them to the active sprint." : "Plan this sprint by dragging issues from the backlog, or create new ones below."}
                footer={canCreate && <InlineCreate projectKey={project.key} sprintId={sprint.id} />}
                actions={
                  manage && (
                    <>
                      {isActive ? (
                        <Button size="sm" onClick={() => setDialog({ kind: "complete", sprint })}>
                          Complete sprint
                        </Button>
                      ) : (
                        <Button size="sm" variant={hasActive ? "secondary" : "primary"} disabled={hasActive} title={hasActive ? "Complete the active sprint before starting another" : undefined} onClick={() => setDialog({ kind: "start", sprint })}>
                          Start sprint
                        </Button>
                      )}
                      <SprintMenu sprint={sprint} onEdit={() => setDialog({ kind: "edit", sprint })} onDelete={() => deleteSprint(sprint, list.length)} />
                    </>
                  )
                }
              />
            );
          })}
          <BacklogSection
            id={BACKLOG}
            title="Backlog"
            issues={issues}
            items={dnd.items[BACKLOG] ?? []}
            collapsed={collapsed.has(BACKLOG)}
            onToggle={() => toggle(BACKLOG)}
            onOpen={onOpen}
            canDrag={canDrag}
            highlight={!!active && target === BACKLOG}
            rowActions={scrum ? rowActions : undefined}
            empty={hasFilters(filters) ? "No backlog issues match these filters." : "Your backlog is empty."}
            footer={canCreate && <InlineCreate projectKey={project.key} />}
          />
        </div>
        <DragOverlay dropAnimation={{ duration: 160, easing: "cubic-bezier(0.2, 0, 0, 1)" }}>{active ? <IssueRow issue={active} lifted /> : null}</DragOverlay>
      </DndContext>
      <EditSprintDialog open={dialog?.kind === "edit"} onClose={() => setDialog(null)} sprint={dialog?.sprint ?? null} projectKey={project.key} />
      <StartSprintDialog
        open={dialog?.kind === "start"}
        onClose={() => setDialog(null)}
        sprint={dialog?.sprint ?? null}
        projectKey={project.key}
        {...startStats(data.sprints.find((s) => s.sprint.id === dialog?.sprint.id)?.issues ?? [])}
      />
      <CompleteSprintDialog open={dialog?.kind === "complete"} onClose={() => setDialog(null)} sprint={dialog?.sprint ?? null} projectKey={project.key} />
    </>
  );
}

function startStats(issues: Issue[]) {
  return { issueCount: issues.length, points: sumPoints(issues).total };
}

function SprintMenu({ sprint, onEdit, onDelete }: { sprint: Sprint; onEdit: () => void; onDelete: () => void }) {
  return (
    <Popover
      align="end"
      trigger={({ toggle }) => (
        <IconButton label={`${sprint.name} actions`} size="sm" onClick={toggle}>
          <MoreHorizontal className="size-4" />
        </IconButton>
      )}
      content={(close) => (
        <div role="menu" className="w-44">
          <MenuItem icon={<Pencil className="size-4" />} onClick={() => (close(), onEdit())}>
            Edit sprint
          </MenuItem>
          {sprint.state === "future" && (
            <MenuItem danger icon={<Trash2 className="size-4" />} onClick={() => (close(), onDelete())}>
              Delete sprint
            </MenuItem>
          )}
        </div>
      )}
    />
  );
}
