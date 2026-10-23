import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, ChevronDown, ChevronRight, Clock, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, EmptyState, IconButton, PageHeader, PageSpinner, Spinner, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Switch, When } from "@/features/settings/common";
import { cleanAction, cleanTrigger, describeAction, describeTrigger, type Rule } from "@/features/settings/automation/meta";
import { RuleDialog } from "@/features/settings/automation/RuleDialog";

const route = getRouteApi("/app/projects/$projectKey/settings/automation");

export function AutomationSettings() {
  const { projectKey } = route.useParams();
  const rules = useQuery({
    queryKey: ["automation-rules", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/automation-rules", { params: { path: { projectKey } } })),
  });
  const [editing, setEditing] = useState<Rule | "new" | null>(null);
  const list = rules.data ?? [];

  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <PageHeader
        title="Automation"
        subtitle="Rules react to issue and development events (or run on a schedule), check an optional condition and run actions."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
            New rule
          </Button>
        }
      />
      {rules.error && <ErrorBox error={rules.error} />}
      {rules.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<Bot className="size-8" />}
            title="No automation rules yet"
            body="For example: when a pull request merges, move the issue to Done and comment with the link; or every Monday, flag issues stuck in review."
            action={<Button onClick={() => setEditing("new")}>New rule</Button>}
          />
        </div>
      ) : (
        <ul className="flex flex-col gap-3">
          {list.map((r) => (
            <RuleCard key={r.id} rule={r} projectKey={projectKey} onEdit={() => setEditing(r)} />
          ))}
        </ul>
      )}
      {editing && <RuleDialog projectKey={projectKey} rule={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} />}
    </div>
  );
}

function RuleCard({ rule, projectKey, onEdit }: { rule: Rule; projectKey: string; onEdit: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [open, setOpen] = useState(false);
  const toggle = useMutation({
    mutationFn: (enabled: boolean) =>
      unwrap(
        api.PUT("/automation-rules/{ruleId}", {
          params: { path: { ruleId: rule.id } },
          body: { name: rule.name, description: rule.description, enabled, trigger: cleanTrigger(rule.trigger), condition: rule.condition, actions: rule.actions.map(cleanAction) },
        }),
      ),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["automation-rules", projectKey] });
      toast.success(`${r.name} ${r.enabled ? "enabled" : "disabled"}`);
    },
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/automation-rules/{ruleId}", { params: { path: { ruleId: rule.id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["automation-rules", projectKey] });
      toast.success(`Deleted ${rule.name}`);
    },
    onError: toast.error,
  });
  const runsId = `runs-${rule.id}`;
  return (
    <li className="rounded-lg border border-line bg-surface">
      <div className="flex flex-wrap items-start gap-3 p-4">
        <span className={`mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md ${rule.enabled ? "bg-accent-soft text-accent" : "bg-surface-2 text-faint"}`}>
          <Bot className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{rule.name}</h3>
            {!rule.enabled && <Badge>Disabled</Badge>}
          </div>
          {rule.description && <p className="text-[13px] text-muted">{rule.description}</p>}
          <p className="mt-1.5 text-[13px]">
            <span className="font-medium">{describeTrigger(rule.trigger)}</span>
            {rule.condition && (
              <>
                {" "}
                <span className="text-muted">if</span> <code className="rounded bg-surface-2 px-1 font-mono text-[12px]">{rule.condition}</code>
              </>
            )}
          </p>
          <ol className="mt-1.5 flex flex-wrap items-center gap-1.5 text-[13px]">
            {rule.actions.map((a, i) => (
              <li key={i} className="inline-flex items-center gap-1.5">
                {i > 0 && <ChevronRight className="size-3.5 text-faint" />}
                <span className="rounded bg-surface-2 px-1.5 py-0.5">{describeAction(a)}</span>
              </li>
            ))}
          </ol>
          <p className="mt-2 flex flex-wrap gap-x-4 text-xs text-muted">
            <span>
              Last run: <When iso={rule.lastRunAt} empty="never" />
            </span>
            {rule.nextRunAt && (
              <span className="inline-flex items-center gap-1">
                <Clock className="size-3" /> Next: <When iso={rule.nextRunAt} />
              </span>
            )}
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Switch checked={rule.enabled} onChange={(v) => toggle.mutate(v)} disabled={toggle.isPending} label={`${rule.enabled ? "Disable" : "Enable"} ${rule.name}`} hideLabel />
          <IconButton label={`Edit ${rule.name}`} size="sm" onClick={onEdit}>
            <Pencil className="size-4" />
          </IconButton>
          <IconButton
            label={`Delete ${rule.name}`}
            size="sm"
            onClick={async () => {
              if (await confirm({ title: `Delete “${rule.name}”?`, body: "The rule and its run history are deleted.", confirmLabel: "Delete rule", danger: true })) remove.mutate();
            }}
          >
            <Trash2 className="size-4" />
          </IconButton>
        </div>
      </div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={runsId}
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-1.5 border-t border-line px-4 py-2 text-left text-[13px] text-muted hover:bg-surface-2/60 hover:text-ink"
      >
        {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />} Recent runs
      </button>
      {open && (
        <div id={runsId}>
          <Runs ruleId={rule.id} />
        </div>
      )}
    </li>
  );
}

const runTone = { success: "success", skipped: "neutral", failed: "danger" } as const;

function Runs({ ruleId }: { ruleId: string }) {
  const runs = useQuery({
    queryKey: ["automation-runs", ruleId],
    queryFn: () => unwrap(api.GET("/automation-rules/{ruleId}/runs", { params: { path: { ruleId }, query: { limit: 20 } } })),
    refetchInterval: 15_000,
  });
  if (runs.isLoading) return <PageSpinner />;
  if (runs.error) return <ErrorBox error={runs.error} className="m-3" />;
  const list = runs.data ?? [];
  return (
    <div className="border-t border-line">
      {list.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">No runs yet. Runs appear here when the trigger fires, including skipped ones (condition not met).</p>
      ) : (
        <ul className="divide-y divide-line">
          {list.map((r) => (
            <li key={r.id} className="flex items-start gap-3 px-4 py-2 text-[13px]">
              <Badge tone={runTone[r.status]} className="mt-0.5 w-16 justify-center">
                {r.status}
              </Badge>
              {r.issueKey ? (
                <Link to="/browse/$issueKey" params={{ issueKey: r.issueKey }} className="mt-0.5 shrink-0 font-mono text-xs text-accent hover:underline">
                  {r.issueKey}
                </Link>
              ) : (
                <span className="mt-0.5 w-14 shrink-0 text-xs text-faint">—</span>
              )}
              <span className="min-w-0 flex-1 break-words text-muted">{r.message || "—"}</span>
              {r.depth > 0 && (
                <span className="shrink-0 text-xs text-faint" title="Triggered by another rule">
                  chain {r.depth}
                </span>
              )}
              <span className="shrink-0 text-xs text-muted">
                <When iso={r.createdAt} />
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="flex justify-end border-t border-line px-4 py-1.5">
        <Button size="sm" variant="ghost" icon={runs.isFetching ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />} onClick={() => runs.refetch()}>
          Refresh
        </Button>
      </div>
    </div>
  );
}
