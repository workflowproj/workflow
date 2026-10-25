import { Fragment, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Pencil, Plus, RefreshCw, Send, Trash2, Webhook as WebhookIcon } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, EmptyState, IconButton, PageHeader, PageSpinner, Spinner, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { SettingsSection, Switch, When } from "../common";
import { SignatureHelp, WebhookDialog, WEBHOOK_EVENTS, type Webhook } from "./WebhookDialog";

/**
 * WebhooksManager lists outbound webhooks of a project (with `projectKey`) or of the whole
 * organisation, with test deliveries and the delivery log.
 */
export function WebhooksManager({ projectKey }: { projectKey?: string }) {
  const hooks = useQuery({
    queryKey: ["webhooks", projectKey ?? ""],
    queryFn: () => unwrap(api.GET("/webhooks", { params: { query: { project: projectKey } } })),
  });
  const [editing, setEditing] = useState<Webhook | "new" | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const list = hooks.data ?? [];

  return (
    <div className="flex max-w-5xl flex-col gap-5">
      <PageHeader
        title="Webhooks"
        subtitle={projectKey ? `Send this project's events to other systems as signed HTTP requests.` : "Organisation-wide webhooks receive events from every project."}
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
            New webhook
          </Button>
        }
      />
      {hooks.error && <ErrorBox error={hooks.error} />}
      {hooks.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<WebhookIcon className="size-8" />}
            title="No webhooks"
            body="Create one to push issue, comment, sprint and release events to chat, CI or your own services."
            action={<Button onClick={() => setEditing("new")}>New webhook</Button>}
          />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>Webhook</th>
                <th>Events</th>
                <th>Active</th>
                <th className="w-40">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {list.map((w) => (
                <Fragment key={w.id}>
                  <HookRow hook={w} projectKey={projectKey} expanded={expanded === w.id} onToggle={() => setExpanded(expanded === w.id ? null : w.id)} onEdit={() => setEditing(w)} />
                  {expanded === w.id && (
                    <tr>
                      <td colSpan={4} className="!p-0">
                        <Deliveries hookId={w.id} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <SettingsSection title="Verifying deliveries">
        <div className="p-4">
          <SignatureHelp />
        </div>
      </SettingsSection>
      {editing && <WebhookDialog projectKey={projectKey} webhook={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
    </div>
  );
}

function HookRow({ hook: w, projectKey, expanded, onToggle, onEdit }: { hook: Webhook; projectKey?: string; expanded: boolean; onToggle: () => void; onEdit: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const key = ["webhooks", projectKey ?? ""];
  const setActive = useMutation({
    mutationFn: (active: boolean) =>
      unwrap(api.PUT("/webhooks/{webhookId}", { params: { path: { webhookId: w.id } }, body: { name: w.name, url: w.url, events: w.events as never, active, ...(projectKey ? { projectKey } : {}) } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    onError: toast.error,
  });
  const test = useMutation({
    mutationFn: () => unwrap(api.POST("/webhooks/{webhookId}/test", { params: { path: { webhookId: w.id } } })),
    onSuccess: () => {
      toast.success("Test delivery queued");
      if (!expanded) onToggle();
      setTimeout(() => qc.invalidateQueries({ queryKey: ["webhook-deliveries", w.id] }), 1500);
    },
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/webhooks/{webhookId}", { params: { path: { webhookId: w.id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: key });
      toast.success(`Deleted ${w.name}`);
    },
    onError: toast.error,
  });
  const labels = w.events.includes("*") ? ["All events"] : w.events.map((e) => WEBHOOK_EVENTS.find((x) => x.id === e)?.label ?? e);
  return (
    <tr className={expanded ? "bg-surface-2/50" : undefined}>
      <td className="max-w-sm">
        <button type="button" onClick={onToggle} aria-expanded={expanded} className="flex items-start gap-1.5 text-left">
          {expanded ? <ChevronDown className="mt-0.5 size-4 shrink-0 text-muted" /> : <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted" />}
          <span className="min-w-0">
            <span className="block font-medium">{w.name}</span>
            <span className="block truncate font-mono text-xs text-muted">{w.url}</span>
          </span>
        </button>
      </td>
      <td>
        <div className="flex max-w-xs flex-wrap gap-1">
          {labels.map((l) => (
            <Badge key={l} className="!normal-case">
              {l}
            </Badge>
          ))}
        </div>
      </td>
      <td>
        <Switch checked={w.active} onChange={(v) => setActive.mutate(v)} disabled={setActive.isPending} label={`${w.name} active`} hideLabel />
      </td>
      <td className="text-right whitespace-nowrap">
        <Button
          size="sm"
          icon={<Send className="size-3.5" />}
          loading={test.isPending}
          onClick={() => test.mutate()}
          disabled={!w.active}
          title={w.active ? "Send a ping event" : "Activate the webhook to test it"}
        >
          Send test
        </Button>
        <IconButton label={`Edit ${w.name}`} size="sm" onClick={onEdit}>
          <Pencil className="size-4" />
        </IconButton>
        <IconButton
          label={`Delete ${w.name}`}
          size="sm"
          onClick={async () => {
            if (await confirm({ title: `Delete ${w.name}?`, body: "Deliveries stop immediately and the delivery log is removed.", confirmLabel: "Delete webhook", danger: true })) remove.mutate();
          }}
        >
          <Trash2 className="size-4" />
        </IconButton>
      </td>
    </tr>
  );
}

const statusTone = { delivered: "success", failed: "danger", pending: "warning" } as const;

function Deliveries({ hookId }: { hookId: string }) {
  const q = useQuery({
    queryKey: ["webhook-deliveries", hookId],
    queryFn: () => unwrap(api.GET("/webhooks/{webhookId}/deliveries", { params: { path: { webhookId: hookId }, query: { limit: 25 } } })),
    refetchInterval: (query) => (query.state.data?.some((d) => d.status === "pending") ? 2000 : 15_000),
  });
  return (
    <div className="border-t border-line bg-surface-2/40 px-4 py-3">
      <div className="mb-2 flex items-center justify-between">
        <p className="text-xs font-semibold tracking-wide text-faint uppercase">Recent deliveries</p>
        <Button size="sm" variant="ghost" icon={q.isFetching ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />} onClick={() => q.refetch()}>
          Refresh
        </Button>
      </div>
      {q.error && <ErrorBox error={q.error} />}
      {q.isLoading ? (
        <PageSpinner />
      ) : (q.data ?? []).length === 0 ? (
        <p className="text-sm text-muted">No deliveries yet. Use “Send test” to queue a ping.</p>
      ) : (
        <table className={`${table} rounded-md bg-surface`}>
          <thead>
            <tr>
              <th>Status</th>
              <th>Event</th>
              <th>Response</th>
              <th>Attempts</th>
              <th>When</th>
            </tr>
          </thead>
          <tbody>
            {(q.data ?? []).map((d) => (
              <tr key={d.id}>
                <td>
                  <Badge tone={statusTone[d.status]}>{d.status}</Badge>
                </td>
                <td className="font-mono text-xs">{d.event}</td>
                <td className="max-w-xs text-xs">
                  {d.responseStatus && <span className={d.responseStatus < 300 ? "text-success" : "text-danger"}>HTTP {d.responseStatus}</span>}
                  {d.lastError && (
                    <span className="block truncate text-danger" title={d.lastError}>
                      {d.lastError}
                    </span>
                  )}
                  {!d.responseStatus && !d.lastError && <span className="text-faint">—</span>}
                </td>
                <td>{d.attempts}</td>
                <td className="text-muted">
                  <When iso={d.deliveredAt ?? d.createdAt} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
