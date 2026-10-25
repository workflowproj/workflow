import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, ChevronDown, ChevronRight, Clock, Pencil, Plus, RefreshCw, Sparkles, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, IconButton, Modal, PageHeader, PageSpinner, Spinner, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Switch, When } from "@/features/settings/common";
import { describeRule } from "@/features/settings/automation/describe";
import { cleanAction, cleanTrigger, type Rule } from "@/features/settings/automation/meta";
import { RuleSentenceView } from "@/features/settings/automation/RuleSentence";
import { TemplateGallery } from "@/features/settings/automation/TemplateGallery";
import type { RuleTemplate } from "@/features/settings/automation/templates";
import { chatChannelsQuery } from "@/features/settings/chat/meta";
import { useProjectStatuses } from "@/features/settings/queries";
import { RuleDialog } from "@/features/settings/automation/RuleDialog";

const route = getRouteApi("/app/projects/$projectKey/settings/automation");

export function AutomationSettings() {
  const { projectKey } = route.useParams();
  const rules = useQuery({
    queryKey: ["automation-rules", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/automation-rules", { params: { path: { projectKey } } })),
  });
  const project = useProjectStatuses(projectKey);
  const [dialog, setDialog] = useState<{ rule?: Rule; template?: RuleTemplate } | null>(null);
  const [gallery, setGallery] = useState(false);
  const list = rules.data ?? [];
  const existingNames = new Set(list.map((r) => r.name.toLowerCase()));
  const statusNames = project.loading ? null : project.statuses.map((s) => s.name);
  const chat = useQuery(chatChannelsQuery(projectKey));
  const channels = chat.data ?? [];
  const pick = (t: RuleTemplate) => {
    setGallery(false);
    // Chat templates post to the project's first channel; it can be changed in the dialog.
    const rule = t.needsChat ? { ...t.rule, actions: t.rule.actions.map((a) => (a.type === "chat" && !a.channelId ? { ...a, channelId: channels[0]?.id ?? "" } : a)) } : t.rule;
    setDialog({ template: { ...t, rule } });
  };

  return (
    <div className="flex max-w-5xl flex-col gap-5">
      <PageHeader
        title="Automation"
        subtitle="Rules react to issue and development events (or run on a schedule), check an optional condition and run actions on the issue, its parent or its children."
        actions={
          <>
            {list.length > 0 && (
              <Button icon={<Sparkles className="size-4" />} onClick={() => setGallery(true)}>
                Start from a template
              </Button>
            )}
            <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setDialog({})}>
              New rule
            </Button>
          </>
        }
      />
      {rules.error && <ErrorBox error={rules.error} />}
      {rules.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <section className="rounded-lg border border-line bg-surface">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3">
            <div className="flex items-center gap-2.5">
              <span className="flex size-8 items-center justify-center rounded-md bg-accent-soft text-accent">
                <Bot className="size-4" />
              </span>
              <div>
                <h2 className="text-sm font-semibold">No automation rules yet — start from a template</h2>
                <p className="text-[13px] text-muted">Each template opens pre-filled so you can review it before creating the rule.</p>
              </div>
            </div>
            <Button size="sm" onClick={() => setDialog({})}>
              Build a rule from scratch
            </Button>
          </div>
          <div className="p-4">
            <TemplateGallery onPick={pick} existingNames={existingNames} statusNames={statusNames} hasChat={channels.length > 0} />
          </div>
        </section>
      ) : (
        <ul className="flex flex-col gap-3">
          {list.map((r) => (
            <RuleCard key={r.id} rule={r} projectKey={projectKey} onEdit={() => setDialog({ rule: r })} />
          ))}
        </ul>
      )}
      <Modal open={gallery} onClose={() => setGallery(false)} title="Start from a template" size="xl">
        <TemplateGallery onPick={pick} existingNames={existingNames} statusNames={statusNames} hasChat={channels.length > 0} />
      </Modal>
      {dialog && <RuleDialog projectKey={projectKey} rule={dialog.rule} initial={dialog.template?.rule} templateTitle={dialog.template?.title} onClose={() => setDialog(null)} />}
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
  const chat = useQuery({ ...chatChannelsQuery(projectKey), enabled: rule.actions.some((a) => a.type === "chat") });
  // A channel missing from a loaded list was removed; say so instead of a generic "chat".
  const channelName = (id: string) => (chat.data ? (chat.data.find((c) => c.id === id)?.name ?? null) : undefined);
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
          <RuleSentenceView sentence={describeRule(rule, { channel: channelName })} className="mt-1.5" />
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
