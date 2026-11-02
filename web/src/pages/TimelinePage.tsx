import { useCallback, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { GanttChart, Pencil, Save, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useMe } from "@/api/queries";
import { Button, EmptyState, Spinner, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { Timeline } from "@/features/timeline/Timeline";
import { TimelineViewDialog } from "@/features/timeline/TimelineViewDialog";
import { ProjectPicker } from "@/features/timeline/Toolbar";
import { ViewSwitcher } from "@/features/timeline/ViewSwitcher";
import { canChangeView, pickView, readLastView, rememberView, splitKeys, timelineViewsQuery, type TimelineView } from "@/features/timeline/views";

const route = getRouteApi("/app/timeline");

/**
 * TimelinePage (route "/app/timeline") shows a saved timeline (`?view=`), a group of projects, or
 * an unsaved choice of projects (`?projects=`). It never shows every project at once: without
 * either, it opens your last-used saved timeline, or asks you to choose projects.
 */
export function TimelinePage() {
  const { view: viewId, projects, jql, issue } = route.useSearch();
  const navigate = route.useNavigate();
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const { data: me } = useMe();
  const { data: views, error } = useQuery(timelineViewsQuery);
  const [dialog, setDialog] = useState<{ view?: TimelineView; initial?: string[] } | null>(null);
  const keys = splitKeys(projects);
  const view = viewId && views ? views.find((v) => v.id === viewId) : undefined;
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  const show = (v: TimelineView) => navigate({ search: (s) => ({ view: v.id, jql: s.jql }) });
  const choose = (k: string[]) => navigate({ search: (s) => ({ jql: s.jql, projects: k.length ? k.join(",") : undefined }) });

  // With neither a saved timeline nor projects chosen, open one of your timelines.
  const scoped = !!viewId || keys.length > 0;
  useEffect(() => {
    if (scoped || !views) return;
    const v = pickView(views, readLastView());
    if (v) navigate({ search: (s) => ({ ...s, view: v.id }), replace: true });
  }, [scoped, views, navigate]);
  useEffect(() => {
    if (view) rememberView(view.id);
  }, [view?.id]);

  const remove = useMutation({
    mutationFn: (v: TimelineView) => unwrap(api.DELETE("/timeline-views/{viewId}", { params: { path: { viewId: v.id } } })),
    onSuccess: (_, v) => {
      qc.setQueryData<TimelineView[]>(["timeline-views"], (list) => list?.filter((x) => x.id !== v.id));
      qc.invalidateQueries({ queryKey: ["timeline-views"] });
      toast.success(`Deleted "${v.name}"`);
      navigate({ search: {} });
    },
    onError: (err) => toast.error(err),
  });
  const askDelete = async (v: TimelineView) => {
    if (await confirm({ title: "Delete timeline?", body: `"${v.name}" will be deleted${v.shared ? " for everyone it is shared with" : ""}. Its projects and their work aren't affected.`, confirmLabel: "Delete", danger: true }))
      remove.mutate(v);
  };

  const shown = view ? view.projects.map((p) => p.key) : viewId ? [] : keys;
  const changeable = view && canChangeView(view, me?.orgRole === "admin");
  const subtitle = view
    ? view.description || "A saved timeline: epics, issues, sprints, releases and the dependencies between them for a group of projects."
    : keys.length > 0
      ? "Not saved yet. Save these projects as a timeline to come back to them."
      : "Plan a group of projects together: epics, issues, sprints, releases and the dependencies between them.";

  let body;
  if (error) body = <ErrorBox error={error} />;
  else if (viewId && views && !view)
    body = (
      <EmptyState
        icon={<GanttChart className="size-8" />}
        title="This timeline isn't available"
        body="It was deleted, or it's no longer shared with you."
        action={<Button onClick={() => navigate({ search: {} })}>Open your timelines</Button>}
      />
    );
  else if (view && view.projects.length === 0)
    body = <EmptyState icon={<GanttChart className="size-8" />} title="No projects to show" body="You can't browse any of this timeline's projects." />;
  else if (shown.length > 0)
    body = (
      <Timeline
        projects={shown.join(",")}
        multi
        baseJql={jql}
        onOpen={openIssue}
        live
        picker={view ? undefined : <ProjectPicker allowAll={false} excludeProgrammes value={keys} onChange={choose} />}
      />
    );
  else if (views && views.length === 0)
    body = (
      <EmptyState
        icon={<GanttChart className="size-8" />}
        title="Timelines show a group of projects together"
        body="Choose the projects to plan together, then save them as a timeline to come back to: for yourself, or shared with everyone."
        action={
          <div className="flex flex-wrap items-center justify-center gap-2">
            <ProjectPicker allowAll={false} excludeProgrammes value={[]} onChange={choose} />
            <Button variant="primary" onClick={() => setDialog({})}>
              New timeline
            </Button>
          </div>
        }
      />
    );
  else
    body = (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="relative z-40 flex shrink-0 flex-wrap items-start gap-3 border-b border-line bg-surface px-4 pt-4 pb-3 md:px-6">
        <div className="min-w-0 flex-1">
          <h1 className="text-lg leading-tight font-semibold">{view ? view.name : "Timeline"}</h1>
          <p className="text-xs text-muted">{subtitle}</p>
          {view && view.hiddenProjects > 0 && (
            <p className="text-xs text-muted">
              {view.hiddenProjects === 1 ? "1 project you can't browse is left out." : `${view.hiddenProjects} projects you can't browse are left out.`}
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!view && keys.length > 0 && (
            <Button size="sm" variant="primary" icon={<Save className="size-3.5" />} onClick={() => setDialog({ initial: keys })}>
              Save as timeline
            </Button>
          )}
          {changeable && (
            <>
              <Button size="sm" icon={<Pencil className="size-3.5" />} onClick={() => setDialog({ view })}>
                Edit
              </Button>
              <Button size="sm" icon={<Trash2 className="size-3.5" />} loading={remove.isPending} onClick={() => askDelete(view)}>
                Delete
              </Button>
            </>
          )}
          {views && <ViewSwitcher views={views} current={view} onPick={show} onNew={() => setDialog({ initial: view ? [] : keys })} />}
        </div>
      </div>
      <div className="min-h-0 flex-1">{body}</div>
      <TimelineViewDialog open={!!dialog} onClose={() => setDialog(null)} view={dialog?.view} initialProjects={dialog?.initial} onSaved={show} />
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}
