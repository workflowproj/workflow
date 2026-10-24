import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { fieldsQuery } from "@/api/queries";
import { Button, Checkbox, Field, Input, Modal, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CelHelp, type CelExample } from "../CelHelp";
import { Callout } from "../common";
import { chatChannelsQuery } from "../chat/meta";
import { useProjectStatuses } from "../queries";
import { ActionRow } from "./ActionFields";
import { describeRule } from "./describe";
import { ACTIONS, cleanAction, cleanTrigger, emptyAction, namedStatuses, TRIGGERS, type Action, type ActionType, type Rule, type RuleInput, type Trigger } from "./meta";
import { RuleSentenceView } from "./RuleSentence";
import { TriggerFields } from "./TriggerFields";

const conditionExamples: CelExample[] = [
  { label: "Only bugs", expr: 'issue.type == "bug"' },
  { label: "High priority or above", expr: 'issue.priority in ["highest", "high"]' },
  { label: "Unassigned issues", expr: 'issue.assignee == ""' },
  { label: "A story whose epic has not started", expr: 'issue.typeLevel == 0 && issue.parent.category == "todo"' },
  { label: "The last open sub-task of its parent", expr: "issue.typeLevel == -1 && issue.parent.openChildren == 0" },
  { label: "Release branches only (development events)", expr: 'event.branch.startsWith("release/")' },
  { label: "Failed pipelines only", expr: 'event.status == "failed"' },
];

/**
 * RuleDialog builds or edits an automation rule: trigger, optional CEL condition and ordered
 * actions, each applied to the issue, its parent or its children. `initial` pre-fills a new
 * rule (e.g. from a template) for review.
 */
export function RuleDialog({ projectKey, rule, initial, templateTitle, onClose }: { projectKey: string; rule?: Rule; initial?: RuleInput; templateTitle?: string; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const project = useProjectStatuses(projectKey);
  const { data: fields = [] } = useQuery(fieldsQuery);
  const start = rule ?? initial;
  const [name, setName] = useState(start?.name ?? "");
  const [description, setDescription] = useState(start?.description ?? "");
  const [enabled, setEnabled] = useState(start?.enabled ?? true);
  const [trigger, setTrigger] = useState<Trigger>(start?.trigger ?? { type: "issue.transitioned" });
  const [condition, setCondition] = useState(start?.condition ?? "");
  const [actions, setActions] = useState<Action[]>(start?.actions ?? [emptyAction("comment")]);

  const usesChat = actions.some((a) => a.type === "chat");
  const chat = useQuery({ ...chatChannelsQuery(projectKey), enabled: usesChat });
  const chatChannels = (chat.data ?? []).map((c) => ({ id: c.id, name: c.name, provider: c.provider }));
  const statusNames = useMemo(() => project.statuses.map((s) => s.name), [project.statuses]);
  // Named statuses that no workflow of this project has (only once the workflows are loaded).
  const missing = useMemo(() => {
    if (project.loading) return new Set<string>();
    const known = new Set(statusNames.map((s) => s.toLowerCase()));
    return new Set(
      namedStatuses({ trigger, actions })
        .map((s) => s.toLowerCase())
        .filter((s) => !known.has(s)),
    );
  }, [project.loading, statusNames, trigger, actions]);
  const missingNames = namedStatuses({ trigger, actions }).filter((s) => missing.has(s.toLowerCase()));

  const body = (): RuleInput => ({ name: name.trim(), description, enabled, trigger: cleanTrigger(trigger), condition: condition.trim(), actions: actions.map(cleanAction) });
  const save = useMutation({
    mutationFn: () =>
      rule
        ? unwrap(api.PUT("/automation-rules/{ruleId}", { params: { path: { ruleId: rule.id } }, body: body() }))
        : unwrap(api.POST("/projects/{projectKey}/automation-rules", { params: { path: { projectKey } }, body: body() })),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["automation-rules", projectKey] });
      toast.success(rule ? `Saved “${r.name}”` : `Created “${r.name}”`);
      onClose();
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const setAction = (i: number, a: Action) => setActions(actions.map((x, k) => (k === i ? a : x)));
  const move = (i: number, d: -1 | 1) => {
    const next = [...actions];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    setActions(next);
  };
  const tinfo = TRIGGERS.find((t) => t.type === trigger.type);
  const shownErrors = new Set(["name", "trigger.type", "trigger.toCategory", "trigger.cron", "trigger.jql", "condition", "actions", ...actions.map((_, i) => `actions[${i}]`)]);
  const otherErrors = save.error && !(save.error instanceof ApiError && Object.keys(fe).some((k) => shownErrors.has(k)));
  const incomplete = actions.some((a) => (a.type === "transition" && !a.category && !a.status) || (a.type === "chat" && !a.channelId));
  const preview = describeRule({ trigger: cleanTrigger(trigger), condition, actions: actions.map(cleanAction) }, { channel: (id) => chatChannels.find((c) => c.id === id)?.name });

  return (
    <Modal
      open
      onClose={onClose}
      title={rule ? "Edit rule" : templateTitle ? "New rule from a template" : "New automation rule"}
      size="xl"
      footer={
        <>
          <Checkbox label="Enabled" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="mr-auto" />
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="rule-form" loading={save.isPending} disabled={!name.trim() || actions.length === 0 || incomplete}>
            {rule ? "Save rule" : "Create rule"}
          </Button>
        </>
      }
    >
      <form
        id="rule-form"
        className="flex flex-col gap-5"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        {templateTitle && (
          <Callout icon={<Sparkles className="mt-0.5 size-4 shrink-0 text-accent" />} title={`From the template “${templateTitle}”`}>
            Review the trigger, condition and actions, adjust them to this project, then create the rule.
          </Callout>
        )}
        {missingNames.length > 0 && (
          <Callout tone="warning" title={`${missingNames.map((s) => `“${s}”`).join(", ")} ${missingNames.length === 1 ? "is not a status" : "are not statuses"} in this project's workflows`}>
            Pick a status the project uses below, or a status category, which works with any workflow.
          </Callout>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" required error={fe.name}>
            {(id) => <Input id={id} autoFocus value={name} maxLength={120} onChange={(e) => setName(e.target.value)} placeholder="Close issues when their PR merges" />}
          </Field>
          <Field label="Description">{(id) => <Input id={id} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />}</Field>
        </div>

        <Step n={1} title="When" hint={tinfo?.hint}>
          <TriggerFields
            trigger={trigger}
            onChange={setTrigger}
            statuses={statusNames}
            customFields={fields.map((f) => ({ key: f.key, name: f.name }))}
            errors={fe}
            projectKey={projectKey}
            missing={missing}
          />
        </Step>

        <Step n={2} title="If (optional)" hint="A CEL expression; the actions run only when it is true. Leave empty to always run.">
          <Textarea
            aria-label="Condition (CEL)"
            rows={2}
            spellCheck={false}
            value={condition}
            onChange={(e) => setCondition(e.target.value)}
            className="font-mono text-[12px]"
            placeholder='issue.type == "bug" && issue.priority == "highest"'
            invalid={!!fe.condition}
          />
          {fe.condition && <p className="mt-1 text-xs text-danger">{fe.condition}</p>}
          <CelHelp className="mt-2" context="automation" examples={conditionExamples} onInsert={setCondition} />
        </Step>

        <Step n={3} title="Then" hint="Actions run in order as the automation user, each on the issue, its parent or its children. They can trigger other rules; chains stop after a few levels.">
          <ol className="flex flex-col gap-2">
            {actions.map((a, i) => (
              <ActionRow
                key={i}
                action={a}
                index={i}
                count={actions.length}
                onChange={(x) => setAction(i, x)}
                onMove={(d) => move(i, d)}
                onRemove={() => setActions(actions.filter((_, k) => k !== i))}
                statuses={statusNames}
                customFields={fields.map((f) => f.key)}
                missing={missing}
                chatChannels={chatChannels}
                projectKey={projectKey}
                error={fe[`actions[${i}]`]}
              />
            ))}
          </ol>
          {fe.actions && <p className="mt-1 text-xs text-danger">{fe.actions}</p>}
          <Select aria-label="Add an action" value="" onChange={(e) => e.target.value && setActions([...actions, emptyAction(e.target.value as ActionType)])} className="mt-2 w-56">
            <option value="">+ Add an action…</option>
            {ACTIONS.map((x) => (
              <option key={x.type} value={x.type}>
                {x.label}
              </option>
            ))}
          </Select>
        </Step>

        <div className="rounded-md border border-line bg-surface-2/60 px-3 py-2.5">
          <p className="mb-1 text-[11px] font-semibold tracking-wide text-faint uppercase">Reads as</p>
          <RuleSentenceView sentence={preview} />
        </div>
        {otherErrors && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}

function Step({ n, title, hint, children }: { n: number; title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="flex gap-3">
      <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-accent text-xs font-semibold text-accent-ink">{n}</span>
      <div className="min-w-0 flex-1">
        <h3 className="text-sm font-semibold">{title}</h3>
        {hint && <p className="mb-2 text-xs text-muted">{hint}</p>}
        {children}
      </div>
    </section>
  );
}
