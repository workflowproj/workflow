import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, FolderGit2, Pencil, Plus, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Badge, Button, EmptyState, IconButton, PageHeader, PageSpinner, Spinner, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ScmConnectionDialog } from "@/features/admin/ScmConnectionDialog";
import { CopyField, Switch, When } from "@/features/settings/common";

type Conn = Schemas["ScmConnection"];

export function ScmAdmin() {
  const q = useQuery({ queryKey: ["scm-connections"], queryFn: () => unwrap(api.GET("/admin/scm-connections")) });
  const [dialog, setDialog] = useState<Conn | "new" | null>(null);
  const list = q.data ?? [];
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="GitHub & GitLab"
        subtitle="Connections discover repositories and receive their webhooks; projects then link the repositories they care about."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setDialog("new")}>
            New connection
          </Button>
        }
      />
      {q.error && <ErrorBox error={q.error} />}
      {q.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<FolderGit2 className="size-8" />}
            title="No connections"
            body="Connect a GitHub App or a GitLab instance to show branches, commits, pull requests and pipelines on issues, and to run smart commits."
            action={<Button onClick={() => setDialog("new")}>New connection</Button>}
          />
        </div>
      ) : (
        <ul className="flex flex-col gap-3">
          {list.map((c) => (
            <ConnectionCard key={c.id} conn={c} onEdit={() => setDialog(c)} />
          ))}
        </ul>
      )}
      {dialog && <ScmConnectionDialog connection={dialog === "new" ? undefined : dialog} onClose={() => setDialog(null)} />}
    </div>
  );
}

function ConnectionCard({ conn: c, onEdit }: { conn: Conn; onEdit: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [open, setOpen] = useState(false);
  const patch = useMutation({
    mutationFn: (body: Schemas["ScmConnectionPatch"]) => unwrap(api.PATCH("/admin/scm-connections/{connectionId}", { params: { path: { connectionId: c.id } }, body })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["scm-connections"] }),
    onError: toast.error,
  });
  const sync = useMutation({
    mutationFn: () => unwrap(api.POST("/admin/scm-connections/{connectionId}/sync", { params: { path: { connectionId: c.id } } })),
    onSuccess: () => {
      toast.success("Sync queued: repositories and pull requests are being reconciled");
      setTimeout(() => qc.invalidateQueries({ queryKey: ["scm-connections"] }), 3000);
    },
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/admin/scm-connections/{connectionId}", { params: { path: { connectionId: c.id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["scm-connections"] });
      qc.invalidateQueries({ queryKey: ["scm-repositories"] });
      toast.success(`Deleted ${c.name}`);
    },
    onError: toast.error,
  });
  return (
    <li className="rounded-lg border border-line bg-surface">
      <div className="flex flex-wrap items-start gap-4 p-4">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{c.name}</h3>
            <Badge tone={c.provider === "github" ? "neutral" : "warning"}>{c.provider === "github" ? "GitHub App" : "GitLab"}</Badge>
          </div>
          <p className="mt-0.5 text-[13px] text-muted">
            {c.provider === "github" ? `App ${c.appSlug || c.appId} · ` : ""}
            {c.webUrl} · {c.repositoryCount} repositor{c.repositoryCount === 1 ? "y" : "ies"} · last event <When iso={c.lastEventAt} empty="never" />
          </p>
          <div className="mt-3 max-w-xl">
            <p className="mb-1 text-xs font-medium text-muted">Webhook URL</p>
            <CopyField value={c.webhookUrl} />
          </div>
          <div className="mt-3 flex flex-wrap gap-x-6 gap-y-2">
            {c.provider === "gitlab" && <Switch checked={c.manageHooks} onChange={(v) => patch.mutate({ manageHooks: v })} disabled={patch.isPending} label="Manage project hooks" />}
            <Switch checked={c.writeBack} onChange={(v) => patch.mutate({ writeBack: v })} disabled={patch.isPending} label="Comment on pull requests" />
          </div>
        </div>
        <div className="flex items-center gap-1">
          <Button size="sm" icon={<RefreshCw className="size-3.5" />} loading={sync.isPending} onClick={() => sync.mutate()}>
            Sync now
          </Button>
          <IconButton label={`Edit ${c.name}`} size="sm" onClick={onEdit}>
            <Pencil className="size-4" />
          </IconButton>
          <IconButton
            label={`Delete ${c.name}`}
            size="sm"
            onClick={async () => {
              if (
                await confirm({
                  title: `Delete ${c.name}?`,
                  body: "Its repositories are unlinked from projects and webhooks from it are rejected. Development links already on issues stay.",
                  confirmLabel: "Delete connection",
                  danger: true,
                })
              )
                remove.mutate();
            }}
          >
            <Trash2 className="size-4" />
          </IconButton>
        </div>
      </div>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-1.5 border-t border-line px-4 py-2 text-left text-[13px] text-muted hover:bg-surface-2/60 hover:text-ink"
      >
        {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />} Recent webhook deliveries
      </button>
      {open && <Events connId={c.id} />}
    </li>
  );
}

const statusTone = { processed: "success", ignored: "neutral", failed: "danger", pending: "warning" } as const;

function Events({ connId }: { connId: string }) {
  const qc = useQueryClient();
  const toast = useToast();
  const q = useQuery({
    queryKey: ["scm-events", connId],
    queryFn: () => unwrap(api.GET("/admin/scm-connections/{connectionId}/events", { params: { path: { connectionId: connId }, query: { limit: 30 } } })),
    refetchInterval: 15_000,
  });
  const reprocess = useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/admin/webhook-events/{eventId}/reprocess", { params: { path: { eventId: id } } })),
    onSuccess: () => {
      toast.success("Delivery re-queued");
      setTimeout(() => qc.invalidateQueries({ queryKey: ["scm-events", connId] }), 1500);
    },
    onError: toast.error,
  });
  return (
    <div className="border-t border-line">
      {q.error && <ErrorBox error={q.error} className="m-3" />}
      {q.isLoading ? (
        <PageSpinner />
      ) : (q.data ?? []).length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">No deliveries received yet. Check the webhook URL and secret in the provider.</p>
      ) : (
        <table className={table}>
          <thead>
            <tr>
              <th>Event</th>
              <th>Status</th>
              <th>Attempts</th>
              <th>Received</th>
              <th className="w-28">
                {q.isFetching && <Spinner className="ml-auto size-3.5" />}
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {(q.data ?? []).map((e) => (
              <tr key={e.id}>
                <td>
                  <span className="block font-mono text-xs">{e.eventType}</span>
                  <span className="block font-mono text-[11px] text-faint">{e.deliveryId}</span>
                </td>
                <td>
                  <Badge tone={statusTone[e.status]}>{e.status}</Badge>
                  {e.error && <p className="mt-1 max-w-sm text-xs break-words text-danger">{e.error}</p>}
                </td>
                <td>{e.attempts}</td>
                <td className="text-muted">
                  <When iso={e.receivedAt} />
                </td>
                <td className="text-right">
                  {(e.status === "failed" || e.status === "ignored") && (
                    <Button size="sm" variant="ghost" icon={<RotateCcw className="size-3.5" />} loading={reprocess.isPending && reprocess.variables === e.id} onClick={() => reprocess.mutate(e.id)}>
                      Reprocess
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
