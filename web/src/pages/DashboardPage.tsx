import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getRouteApi, Link, useBlocker, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Ellipsis, Globe, LayoutDashboard, Lock, Pencil, Plus, Trash2 } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { useMe } from "@/api/queries";
import { Badge, Button, Checkbox, EmptyState, IconButton, Input, MenuItem, PageSpinner, Popover, Textarea, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { dashboardQuery } from "@/features/dashboards/api";
import { DashboardGrid } from "@/features/dashboards/DashboardGrid";
import { GadgetConfigDialog, GadgetGallery } from "@/features/dashboards/GadgetDialogs";
import { liveScope, newGadget, sameDashboard, seedScope, toInput, type Dashboard, type DashboardInput } from "@/features/dashboards/model";
import { useCreateDashboard } from "@/features/dashboards/NewDashboard";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { issueQuery } from "@/features/issues/issue-api";
import { useLiveEvents, useProjectEvents } from "@/lib/events";
import { formatDateTime, relativeTime } from "@/lib/format";

const route = getRouteApi("/app/dashboards/$dashboardId");

/** DashboardPage shows one dashboard (route "/app/dashboards/$dashboardId", `?edit=true` to edit). */
export function DashboardPage() {
  const { dashboardId } = route.useParams();
  const { edit, issue } = route.useSearch();
  const navigate = route.useNavigate();
  const { data: dashboard, isLoading, error } = useQuery(dashboardQuery(dashboardId));
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  // Keep the issue open in the drawer live; its changes also refresh the gadgets.
  const { data: open } = useQuery({ ...issueQuery(issue ?? ""), enabled: !!issue });
  useProjectEvents(open?.projectKey);
  // One stream for every project the gadgets read; bursts refresh the gadgets once.
  const scope = useMemo(() => liveScope(dashboard?.gadgets ?? []), [dashboard]);
  useLiveEvents(scope, { debounceMs: 2000 });

  if (isLoading) return <PageSpinner label="Loading dashboard" />;
  if (error || !dashboard) {
    const missing = error instanceof ApiError && (error.status === 404 || error.status === 403);
    return (
      <div className="mx-auto max-w-3xl p-6">
        {missing ? (
          <EmptyState
            icon={<LayoutDashboard className="size-8" />}
            title="Dashboard not found"
            body="It may have been deleted, or its owner stopped sharing it."
            action={
              <Link to="/dashboards" className="text-sm font-medium text-accent hover:underline">
                All dashboards
              </Link>
            }
          />
        ) : (
          <ErrorBox error={error} />
        )}
      </div>
    );
  }
  return (
    <div className="mx-auto max-w-[1600px] p-4 md:p-6">
      {edit && dashboard.canEdit ? (
        <DashboardEditor dashboard={dashboard} openIssue={openIssue} />
      ) : (
        <DashboardView dashboard={dashboard} openIssue={openIssue} onEdit={() => navigate({ search: (s) => ({ ...s, edit: true }) })} />
      )}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

function Visibility({ shared }: { shared: boolean }) {
  return shared ? (
    <Badge tone="accent">
      <Globe className="size-3" /> Shared
    </Badge>
  ) : (
    <Badge>
      <Lock className="size-3" /> Private
    </Badge>
  );
}

function DashboardView({ dashboard: d, openIssue, onEdit }: { dashboard: Dashboard; openIssue: (key: string) => void; onEdit: () => void }) {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const toast = useToast();
  const copy = useCreateDashboard();
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/dashboards/{dashboardId}", { params: { path: { dashboardId: d.id } } })),
    onSuccess: () => {
      qc.removeQueries({ queryKey: ["dashboards", d.id] });
      qc.invalidateQueries({ queryKey: ["dashboards"], exact: true });
      toast.success(`Deleted "${d.name}"`);
      navigate({ to: "/dashboards" });
    },
    onError: toast.error,
  });
  const makeCopy = () =>
    copy.mutate(
      { name: `Copy of ${d.name}`.slice(0, 80), description: d.description, shared: false, gadgets: d.gadgets },
      { onSuccess: () => toast.success("Copied. This copy is yours to change."), onError: toast.error },
    );
  const askDelete = async () => {
    if (await confirm({ title: `Delete "${d.name}"?`, body: d.shared ? "It is shared, so it disappears for everyone. This can't be undone." : "This can't be undone.", confirmLabel: "Delete dashboard", danger: true })) remove.mutate();
  };
  const mine = d.owner.id === me?.id;
  return (
    <>
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to="/dashboards" className="text-[13px] text-muted hover:text-ink">
            Dashboards
          </Link>
          <div className="mt-0.5 flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 truncate text-xl font-semibold tracking-tight">{d.name}</h1>
            <Visibility shared={d.shared} />
          </div>
          {d.description && <p className="mt-1 max-w-3xl text-sm text-muted">{d.description}</p>}
          <p className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs text-muted">
            <UserAvatar user={d.owner} size={18} />
            {mine ? "Your dashboard" : `Owned by ${d.owner.displayName}`}
            <span aria-hidden>·</span>
            <span title={formatDateTime(d.updatedAt)}>Updated {relativeTime(d.updatedAt)}</span>
            {!d.canEdit && (
              <>
                <span aria-hidden>·</span>View only
              </>
            )}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {d.canEdit ? (
            <>
              <Button icon={<Pencil className="size-3.5" />} onClick={onEdit}>
                Edit
              </Button>
              <Popover
                align="end"
                trigger={({ toggle, open }) => (
                  <IconButton label="More dashboard actions" aria-haspopup="menu" aria-expanded={open} onClick={toggle} className="border border-line-strong">
                    <Ellipsis className="size-4" />
                  </IconButton>
                )}
                content={(close) => (
                  <div role="menu">
                    <MenuItem icon={<Copy className="size-4" />} onClick={() => (close(), makeCopy())}>
                      Make a copy
                    </MenuItem>
                    <MenuItem icon={<Trash2 className="size-4" />} danger onClick={() => (close(), void askDelete())}>
                      Delete dashboard
                    </MenuItem>
                  </div>
                )}
              />
            </>
          ) : (
            <Button icon={<Copy className="size-3.5" />} loading={copy.isPending} onClick={makeCopy} title="Make your own copy to change">
              Make a copy
            </Button>
          )}
        </div>
      </div>
      {d.gadgets.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<LayoutDashboard className="size-8" />}
            title="No gadgets yet"
            body={d.canEdit ? "Add burndowns, velocity, flow charts, numbers and issue lists." : "Its owner hasn't added anything yet."}
            action={
              d.canEdit && (
                <Button variant="primary" icon={<Plus className="size-4" />} onClick={onEdit}>
                  Add gadgets
                </Button>
              )
            }
          />
        </div>
      ) : (
        <DashboardGrid gadgets={d.gadgets} editing={false} openIssue={openIssue} />
      )}
    </>
  );
}

/**
 * DashboardEditor edits a copy of the dashboard and saves it in one PUT. Leaving with unsaved
 * changes asks first; opening an issue from a gadget doesn't count as leaving.
 */
function DashboardEditor({ dashboard, openIssue }: { dashboard: Dashboard; openIssue: (key: string) => void }) {
  const qc = useQueryClient();
  const navigate = route.useNavigate();
  const confirm = useConfirm();
  const toast = useToast();
  const [draft, setDraft] = useState<DashboardInput>(() => toInput(dashboard));
  const [adding, setAdding] = useState(dashboard.gadgets.length === 0);
  const [configuring, setConfiguring] = useState<string | null>(null);
  const [scrollTo, setScrollTo] = useState<string | null>(null);
  const dirty = !sameDashboard(draft, toInput(dashboard));
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const leaving = useRef(false);
  const update = (patch: Partial<DashboardInput>) => setDraft((d) => ({ ...d, ...patch }));
  const exit = () => {
    leaving.current = true;
    navigate({ search: (s) => ({ ...s, edit: undefined }) });
  };

  const save = useMutation({
    mutationFn: (body: DashboardInput) => unwrap(api.PUT("/dashboards/{dashboardId}", { params: { path: { dashboardId: dashboard.id } }, body })),
    onSuccess: (d) => {
      qc.setQueryData(["dashboards", d.id], d);
      qc.invalidateQueries({ queryKey: ["dashboards"], exact: true });
      toast.success("Dashboard saved");
      exit();
    },
  });

  useBlocker({
    shouldBlockFn: async ({ current, next }) => {
      if (leaving.current || !dirtyRef.current) return false;
      if (next.pathname === current.pathname && (next.search as { edit?: boolean }).edit) return false;
      const leave = await confirm({ title: "Discard unsaved changes?", body: "Your changes to this dashboard haven't been saved.", confirmLabel: "Discard changes", danger: true });
      return !leave;
    },
    enableBeforeUnload: () => dirtyRef.current,
  });

  // Bring a newly added gadget into view once it's configured.
  useEffect(() => {
    if (!scrollTo || configuring) return;
    document.querySelector(`[data-gadget-id="${scrollTo}"]`)?.scrollIntoView({ behavior: "smooth", block: "nearest" });
    setScrollTo(null);
  }, [scrollTo, configuring]);

  const cancel = async () => {
    if (dirty && !(await confirm({ title: "Discard unsaved changes?", body: "Your changes to this dashboard haven't been saved.", confirmLabel: "Discard changes", danger: true }))) return;
    exit();
  };
  const fieldErrors = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const configGadget = draft.gadgets.find((g) => g.id === configuring) ?? null;

  return (
    <>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate({ ...draft, name: draft.name.trim(), description: draft.description?.trim() });
        }}
        className="mb-5 rounded-lg border border-accent/40 bg-surface p-4"
      >
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-2" style={{ minWidth: "min(100%, 20rem)" }}>
            <Input aria-label="Dashboard name" value={draft.name} maxLength={80} required invalid={!!fieldErrors.name} onChange={(e) => update({ name: e.target.value })} className="h-9 text-base font-semibold" />
            <Textarea aria-label="Description" rows={1} value={draft.description ?? ""} maxLength={500} placeholder="Description (optional)" onChange={(e) => update({ description: e.target.value })} className="min-h-8.5 resize-y text-[13px]" />
            <Checkbox checked={!!draft.shared} onChange={(e) => update({ shared: e.target.checked })} label="Share with everyone in the organisation" />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
              Add gadget
            </Button>
            <Button onClick={() => void cancel()}>Cancel</Button>
            <Button variant="primary" type="submit" loading={save.isPending} disabled={!draft.name.trim()}>
              Save
            </Button>
          </div>
        </div>
        {save.error ? <ErrorBox error={save.error} className="mt-3" /> : null}
        <p className="mt-3 text-xs text-muted">
          {dirty ? "Unsaved changes. " : ""}Drag gadgets by their handle to rearrange them (or focus the handle and use Space and the arrow keys); the menu on each gadget resizes, moves, duplicates or removes it.
        </p>
      </form>
      {draft.gadgets.length === 0 ? (
        <div className="rounded-lg border border-dashed border-line-strong bg-surface">
          <EmptyState
            icon={<LayoutDashboard className="size-8" />}
            title="Add your first gadget"
            body="Pick from sprint burndowns and burnups, velocity, cumulative flow, created vs resolved, cycle time, breakdowns, numbers and issue lists."
            action={
              <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
                Add gadget
              </Button>
            }
          />
        </div>
      ) : (
        <DashboardGrid gadgets={draft.gadgets} editing onChange={(gadgets) => update({ gadgets })} onConfigure={setConfiguring} openIssue={openIssue} />
      )}
      <GadgetGallery
        open={adding}
        onClose={() => setAdding(false)}
        onPick={(type) => {
          const g = newGadget(type, draft.gadgets, seedScope(draft.gadgets));
          update({ gadgets: [...draft.gadgets, g] });
          setAdding(false);
          setConfiguring(g.id);
          setScrollTo(g.id);
        }}
      />
      <GadgetConfigDialog
        gadget={configGadget}
        onClose={() => setConfiguring(null)}
        onSave={(g) => {
          update({ gadgets: draft.gadgets.map((x) => (x.id === g.id ? g : x)) });
          setConfiguring(null);
        }}
      />
    </>
  );
}
