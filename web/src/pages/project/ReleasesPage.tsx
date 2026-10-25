import { useCallback, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Archive, ArchiveRestore, CalendarDays, ChevronRight, MoreHorizontal, Package, Pencil, Plus, Rocket, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { hasPermission, useProject } from "@/api/queries";
import type { Project } from "@/api/types";
import { Badge, Button, EmptyState, IconButton, MenuItem, PageHeader, PageSpinner, Popover, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { formatDay, formatRange, todayISO } from "@/features/backlog/dates";
import { ReleaseFormDialog, ShipReleaseDialog } from "@/features/releases/ReleaseDialogs";
import { ReleaseIssues } from "@/features/releases/ReleaseIssues";
import { releasesQuery, type Release } from "@/features/releases/queries";
import { cn } from "@/lib/cn";
import { formatDateTime } from "@/lib/format";

const route = getRouteApi("/app/projects/$projectKey/releases");

type Filter = "unreleased" | "released" | "archived" | "all";
const FILTERS: { id: Filter; label: string }[] = [
  { id: "unreleased", label: "Unreleased" },
  { id: "released", label: "Released" },
  { id: "archived", label: "Archived" },
  { id: "all", label: "All" },
];

export function ReleasesPage() {
  const { projectKey } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const { data: project } = useProject(projectKey);
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  return (
    <div className="mx-auto max-w-6xl p-4 md:p-6">
      {project ? <Releases project={project} onOpen={openIssue} /> : <PageSpinner />}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

type Dialog = { kind: "form"; release: Release | null } | { kind: "ship"; release: Release } | null;

function Releases({ project, onOpen }: { project: Project; onOpen: (key: string) => void }) {
  const { data: releases = [], isLoading, error } = useQuery(releasesQuery(project.key));
  const [filter, setFilter] = useState<Filter>("unreleased");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<Dialog>(null);
  const manage = hasPermission(project, "release.manage");
  const counts = Object.fromEntries(FILTERS.map((f) => [f.id, f.id === "all" ? releases.length : releases.filter((r) => r.state === f.id).length])) as Record<Filter, number>;
  const shown = releases.filter((r) => filter === "all" || r.state === filter);
  const toggle = (id: string) => setExpanded((s) => (s.has(id) ? new Set([...s].filter((x) => x !== id)) : new Set(s).add(id)));

  return (
    <>
      <PageHeader
        title="Releases"
        subtitle="Versions of the product and the issues that ship in each."
        actions={
          manage && (
            <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setDialog({ kind: "form", release: null })}>
              Create release
            </Button>
          )
        }
      />
      {error && <ErrorBox error={error} />}
      {isLoading ? (
        <PageSpinner label="Loading releases" />
      ) : releases.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<Package className="size-8" />}
            title="No releases yet"
            body="Create a release for the next version, then set it as the fix version on issues to track its progress."
            action={
              manage && (
                <Button variant="primary" onClick={() => setDialog({ kind: "form", release: null })}>
                  Create release
                </Button>
              )
            }
          />
        </div>
      ) : (
        <div className="rounded-lg border border-line bg-surface">
          <div role="tablist" aria-label="Release status" className="flex gap-1 border-b border-line p-2">
            {FILTERS.map((f) => (
              <button
                key={f.id}
                type="button"
                role="tab"
                aria-selected={filter === f.id}
                onClick={() => setFilter(f.id)}
                className={cn("inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-[13px] font-medium", filter === f.id ? "bg-accent-soft text-accent" : "text-muted hover:bg-surface-2 hover:text-ink")}
              >
                {f.label}
                <span className="text-xs opacity-75 tabular-nums">{counts[f.id]}</span>
              </button>
            ))}
          </div>
          {shown.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-muted">No {filter === "all" ? "" : filter} releases.</p>
          ) : (
            <ul>
              {shown.map((r) => (
                <ReleaseItem
                  key={r.id}
                  release={r}
                  project={project}
                  open={expanded.has(r.id)}
                  onToggle={() => toggle(r.id)}
                  onOpen={onOpen}
                  onEdit={() => setDialog({ kind: "form", release: r })}
                  onShip={() => setDialog({ kind: "ship", release: r })}
                />
              ))}
            </ul>
          )}
        </div>
      )}
      <ReleaseFormDialog open={dialog?.kind === "form"} onClose={() => setDialog(null)} release={dialog?.kind === "form" ? dialog.release : null} projectKey={project.key} />
      <ShipReleaseDialog
        open={dialog?.kind === "ship"}
        onClose={() => setDialog(null)}
        release={dialog?.kind === "ship" ? dialog.release : null}
        others={releases.filter((r) => r.state === "unreleased" && r.id !== dialog?.release?.id)}
        openCount={dialog?.kind === "ship" ? dialog.release.issueCount - dialog.release.doneCount : 0}
        projectKey={project.key}
      />
    </>
  );
}

function ReleaseItem({
  release: r,
  project,
  open,
  onToggle,
  onOpen,
  onEdit,
  onShip,
}: {
  release: Release;
  project: Project;
  open: boolean;
  onToggle: () => void;
  onOpen: (key: string) => void;
  onEdit: () => void;
  onShip: () => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const manage = hasPermission(project, "release.manage");
  const overdue = r.state === "unreleased" && !!r.releaseDate && r.releaseDate < todayISO();
  const invalidate = () => qc.invalidateQueries({ queryKey: ["releases", project.key] });
  const setState = useMutation({
    mutationFn: (state: Release["state"]) => unwrap(api.PATCH("/releases/{releaseId}", { params: { path: { releaseId: r.id } }, body: { state } })),
    onSuccess: (x) => {
      invalidate();
      toast.success(x.state === "archived" ? `${x.name} archived` : `${x.name} restored`);
    },
    onError: (e) => toast.error(e),
  });
  const remove = async () => {
    const ok = await confirm({
      title: `Delete ${r.name}?`,
      body: r.issueCount ? `${r.issueCount} issue${r.issueCount === 1 ? "" : "s"} will lose this fix version.` : "No issues use this release.",
      confirmLabel: "Delete release",
      danger: true,
    });
    if (!ok) return;
    try {
      await unwrap(api.DELETE("/releases/{releaseId}", { params: { path: { releaseId: r.id } } }));
      invalidate();
      qc.invalidateQueries({ queryKey: ["search"] });
      toast.success(`${r.name} deleted`);
    } catch (e) {
      toast.error(e);
    }
  };
  return (
    <li className="border-b border-line last:border-b-0">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 px-3 py-3 sm:flex-nowrap">
        <button type="button" onClick={onToggle} aria-expanded={open} className="flex min-w-0 flex-1 items-center gap-2 rounded text-left focus-visible:outline-2 focus-visible:outline-accent">
          <ChevronRight className={cn("size-4 shrink-0 text-faint transition-transform", open && "rotate-90")} />
          <span className="min-w-0">
            <span className="flex flex-wrap items-center gap-2">
              <span className="font-semibold">{r.name}</span>
              <StateBadge release={r} />
              {overdue && <Badge tone="warning">Overdue</Badge>}
            </span>
            {r.description && <span className="mt-0.5 block truncate text-xs text-muted">{r.description}</span>}
          </span>
        </button>
        <Progress done={r.doneCount} total={r.issueCount} />
        <div className="w-44 shrink-0 text-xs text-muted" title={r.releasedAt ? `Released ${formatDateTime(r.releasedAt)}` : undefined}>
          {r.startDate || r.releaseDate ? (
            <span className="inline-flex items-center gap-1.5">
              <CalendarDays className="size-3.5" />
              {r.startDate ? formatRange(r.startDate, r.releaseDate) : `Due ${formatDay(r.releaseDate!)}`}
            </span>
          ) : (
            <span className="text-faint">No dates</span>
          )}
        </div>
        <div className="flex w-32 shrink-0 items-center justify-end gap-1">
          {manage && r.state === "unreleased" && (
            <Button size="sm" icon={<Rocket className="size-3.5" />} onClick={onShip}>
              Release
            </Button>
          )}
          {manage && (
            <Popover
              align="end"
              trigger={({ toggle }) => (
                <IconButton label={`${r.name} actions`} size="sm" onClick={toggle}>
                  <MoreHorizontal className="size-4" />
                </IconButton>
              )}
              content={(close) => (
                <div role="menu" className="w-44">
                  <MenuItem icon={<Pencil className="size-4" />} onClick={() => (close(), onEdit())}>
                    Edit
                  </MenuItem>
                  {r.state === "archived" ? (
                    <MenuItem icon={<ArchiveRestore className="size-4" />} onClick={() => (close(), setState.mutate(r.releasedAt ? "released" : "unreleased"))}>
                      Restore
                    </MenuItem>
                  ) : (
                    <MenuItem icon={<Archive className="size-4" />} onClick={() => (close(), setState.mutate("archived"))}>
                      Archive
                    </MenuItem>
                  )}
                  <MenuItem danger icon={<Trash2 className="size-4" />} onClick={() => (close(), remove())}>
                    Delete
                  </MenuItem>
                </div>
              )}
            />
          )}
        </div>
      </div>
      {open && (
        <div className="border-t border-line bg-canvas/60">
          <ReleaseIssues projectKey={project.key} release={r} onOpen={onOpen} />
        </div>
      )}
    </li>
  );
}

function StateBadge({ release }: { release: Release }) {
  if (release.state === "released") return <Badge tone="success">Released</Badge>;
  if (release.state === "archived") return <Badge>Archived</Badge>;
  return <Badge tone="accent">Unreleased</Badge>;
}

/** Progress is the share of a release's issues that are done. */
function Progress({ done, total }: { done: number; total: number }) {
  const pct = total ? Math.round((done / total) * 100) : 0;
  return (
    <div className="flex w-52 shrink-0 items-center gap-2.5" title={`${done} of ${total} issues done`}>
      <div role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label="Issues done" className="h-2 flex-1 overflow-hidden rounded-full bg-surface-3">
        <div className="h-full rounded-full bg-done transition-[width]" style={{ width: `${pct}%` }} />
      </div>
      <span className="w-20 text-xs text-muted tabular-nums">
        {total ? (
          <>
            <span className="font-medium text-ink">{done}</span> / {total} done
          </>
        ) : (
          "No issues"
        )}
      </span>
    </div>
  );
}
