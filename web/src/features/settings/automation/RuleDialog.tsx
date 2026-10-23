import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { fieldsQuery } from "@/api/queries";
import { Button, Checkbox, Field, IconButton, Input, Modal, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CelHelp, type CelExample } from "../CelHelp";
import { useProjectStatuses } from "../queries";
import { ACTIONS, CHANGEABLE_FIELDS, cleanAction, cleanTrigger, CRON_PRESETS, emptyAction, SET_FIELDS, TRIGGERS, type Action, type ActionType, type Rule, type RuleInput, type Trigger } from "./meta";

const conditionExamples: CelExample[] = [
  { label: "Only bugs", expr: 'issue.type == "bug"' },
  { label: "High priority or above", expr: 'issue.priority in ["highest", "high"]' },
  { label: "Unassigned issues", expr: 'issue.assignee == ""' },
  { label: "Release branches only", expr: 'event.branch.startsWith("release/")' },
  { label: "Only failed pipelines", expr: 'event.state == "failed"' },
];

const templateVars = ["{{issue.key}}", "{{issue.summary}}", "{{event.from}}", "{{event.to}}", "{{event.title}}", "{{event.url}}"];

/** RuleDialog builds an automation rule: trigger, optional CEL condition and ordered actions. */
export function RuleDialog({ projectKey, rule, onClose }: { projectKey: string; rule?: Rule; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { statuses } = useProjectStatuses(projectKey);
  const { data: fields = [] } = useQuery(fieldsQuery);
  const [name, setName] = useState(rule?.name ?? "");
  const [description, setDescription] = useState(rule?.description ?? "");
  const [enabled, setEnabled] = useState(rule?.enabled ?? true);
  const [trigger, setTrigger] = useState<Trigger>(rule?.trigger ?? { type: "issue.transitioned" });
  const [condition, setCondition] = useState(rule?.condition ?? "");
  const [actions, setActions] = useState<Action[]>(rule?.actions ?? [emptyAction("comment")]);

  const save = useMutation({
    mutationFn: () => {
      const body: RuleInput = { name: name.trim(), description, enabled, trigger: cleanTrigger(trigger), condition: condition.trim(), actions: actions.map(cleanAction) };
      return rule
        ? unwrap(api.PUT("/automation-rules/{ruleId}", { params: { path: { ruleId: rule.id } }, body }))
        : unwrap(api.POST("/projects/{projectKey}/automation-rules", { params: { path: { projectKey } }, body }));
    },
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
  const fieldKeys = [...CHANGEABLE_FIELDS, ...fields.map((f) => f.key)];
  const shownErrors = new Set(["name", "trigger.type", "trigger.cron", "trigger.jql", "condition", "actions", ...actions.map((_, i) => `actions[${i}]`)]);
  const otherErrors = save.error && !(save.error instanceof ApiError && Object.keys(fe).some((k) => shownErrors.has(k)));

  return (
    <Modal
      open
      onClose={onClose}
      title={rule ? `Edit rule` : "New automation rule"}
      size="xl"
      footer={
        <>
          <Checkbox label="Enabled" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="mr-auto" />
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="rule-form" loading={save.isPending} disabled={!name.trim() || actions.length === 0}>
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
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" required error={fe.name}>
            {(id) => <Input id={id} autoFocus value={name} maxLength={120} onChange={(e) => setName(e.target.value)} placeholder="Close issues when their PR merges" />}
          </Field>
          <Field label="Description">{(id) => <Input id={id} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />}</Field>
        </div>

        <Step n={1} title="When" hint={tinfo?.hint}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Trigger" error={fe["trigger.type"]}>
              {(id) => (
                <Select id={id} value={trigger.type} onChange={(e) => setTrigger({ type: e.target.value as Trigger["type"] })}>
                  {(["Issues", "Development", "Schedule"] as const).map((g) => (
                    <optgroup key={g} label={g}>
                      {TRIGGERS.filter((t) => t.group === g).map((t) => (
                        <option key={t.type} value={t.type}>
                          {t.label}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </Select>
              )}
            </Field>
            {trigger.type === "issue.transitioned" && (
              <Field label="To status">
                {(id) => (
                  <Select id={id} value={trigger.toStatus ?? ""} onChange={(e) => setTrigger({ ...trigger, toStatus: e.target.value || undefined })}>
                    <option value="">Any status</option>
                    {statuses.map((s) => (
                      <option key={s.id} value={s.name}>
                        {s.name}
                      </option>
                    ))}
                  </Select>
                )}
              </Field>
            )}
            {trigger.type === "issue.field_changed" && (
              <Field label="Field">
                {(id) => (
                  <Select id={id} value={trigger.field ?? ""} onChange={(e) => setTrigger({ ...trigger, field: e.target.value || undefined })}>
                    <option value="">Any field</option>
                    {fieldKeys.map((f) => (
                      <option key={f} value={f}>
                        {f}
                      </option>
                    ))}
                  </Select>
                )}
              </Field>
            )}
            {trigger.type === "schedule" && (
              <>
                <Field label="Cron (UTC)" error={fe["trigger.cron"]} hint="minute hour day month weekday, or @daily / @hourly">
                  {(id) => (
                    <div className="flex gap-2">
                      <Input
                        id={id}
                        value={trigger.cron ?? ""}
                        onChange={(e) => setTrigger({ ...trigger, cron: e.target.value })}
                        className="font-mono"
                        placeholder="0 9 * * 1-5"
                        invalid={!!fe["trigger.cron"]}
                      />
                      <Select aria-label="Cron presets" value="" onChange={(e) => e.target.value && setTrigger({ ...trigger, cron: e.target.value })} className="!w-32 shrink-0">
                        <option value="">Presets…</option>
                        {CRON_PRESETS.map((p) => (
                          <option key={p.cron} value={p.cron}>
                            {p.label}
                          </option>
                        ))}
                      </Select>
                    </div>
                  )}
                </Field>
                <Field label="Issues (JQL)" error={fe["trigger.jql"]} hint="The actions run once for each matching issue." className="sm:col-span-2">
                  {(id) => (
                    <Input
                      id={id}
                      value={trigger.jql ?? ""}
                      onChange={(e) => setTrigger({ ...trigger, jql: e.target.value })}
                      className="font-mono text-[13px]"
                      placeholder={`project = ${projectKey} AND status = "In Review" AND updated < -7d`}
                      invalid={!!fe["trigger.jql"]}
                    />
                  )}
                </Field>
              </>
            )}
          </div>
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

        <Step n={3} title="Then" hint="Actions run in order as the automation user; each can trigger other rules (loops stop after a few levels).">
          <ol className="flex flex-col gap-2">
            {actions.map((a, i) => (
              <li key={i} className="rounded-md border border-line bg-surface-2/50 p-3">
                <div className="mb-2 flex items-center gap-2">
                  <span className="flex size-5 items-center justify-center rounded-full bg-surface-3 text-[11px] font-semibold">{i + 1}</span>
                  <Select aria-label={`Action ${i + 1} type`} value={a.type} onChange={(e) => setAction(i, emptyAction(e.target.value as ActionType))} className="!h-7.5 !w-52 py-0 text-[13px]">
                    {ACTIONS.map((x) => (
                      <option key={x.type} value={x.type}>
                        {x.label}
                      </option>
                    ))}
                  </Select>
                  <div className="ml-auto flex">
                    <IconButton label={`Move action ${i + 1} up`} size="sm" disabled={i === 0} onClick={() => move(i, -1)}>
                      <ArrowUp className="size-3.5" />
                    </IconButton>
                    <IconButton label={`Move action ${i + 1} down`} size="sm" disabled={i === actions.length - 1} onClick={() => move(i, 1)}>
                      <ArrowDown className="size-3.5" />
                    </IconButton>
                    <IconButton label={`Remove action ${i + 1}`} size="sm" disabled={actions.length === 1} onClick={() => setActions(actions.filter((_, k) => k !== i))}>
                      <X className="size-3.5" />
                    </IconButton>
                  </div>
                </div>
                <ActionFields action={a} index={i} onChange={(x) => setAction(i, x)} statuses={statuses.map((s) => s.name)} customFields={fields.map((f) => f.key)} />
                {fe[`actions[${i}]`] && <p className="mt-1.5 text-xs text-danger">{fe[`actions[${i}]`]}</p>}
              </li>
            ))}
          </ol>
          {fe.actions && <p className="mt-1 text-xs text-danger">{fe.actions}</p>}
          <Select aria-label="Add an action" value="" onChange={(e) => e.target.value && setActions([...actions, emptyAction(e.target.value as ActionType)])} className="mt-2 !w-56">
            <option value="">+ Add an action…</option>
            {ACTIONS.map((x) => (
              <option key={x.type} value={x.type}>
                {x.label}
              </option>
            ))}
          </Select>
        </Step>
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

function ActionFields({ action: a, index, onChange, statuses, customFields }: { action: Action; index: number; onChange: (a: Action) => void; statuses: string[]; customFields: string[] }) {
  const label = `Action ${index + 1}`;
  switch (a.type) {
    case "transition":
      return (
        <Select aria-label={`${label}: target status`} value={a.status ?? ""} onChange={(e) => onChange({ ...a, status: e.target.value })} invalid={!a.status}>
          <option value="">Choose a status…</option>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </Select>
      );
    case "assign": {
      const preset = ["actor", "reporter", "unassigned"].includes(a.assignee ?? "") ? a.assignee : "email";
      return (
        <div className="flex gap-2">
          <Select aria-label={`${label}: assignee`} value={preset} onChange={(e) => onChange({ ...a, assignee: e.target.value === "email" ? "" : e.target.value })} className="max-w-xs">
            <option value="actor">Who triggered the event</option>
            <option value="reporter">The reporter</option>
            <option value="unassigned">Nobody (unassign)</option>
            <option value="email">A specific person…</option>
          </Select>
          {preset === "email" && (
            <Input aria-label={`${label}: email`} type="email" value={a.assignee ?? ""} onChange={(e) => onChange({ ...a, assignee: e.target.value })} placeholder="person@example.com" />
          )}
        </div>
      );
    }
    case "set_field":
      return (
        <div className="grid grid-cols-2 gap-2">
          <Select aria-label={`${label}: field`} value={a.field ?? ""} onChange={(e) => onChange({ ...a, field: e.target.value })}>
            {[...SET_FIELDS, ...customFields].map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </Select>
          {a.field === "priority" ? (
            <Select aria-label={`${label}: value`} value={String(a.value ?? "")} onChange={(e) => onChange({ ...a, value: e.target.value })}>
              {["highest", "high", "medium", "low", "lowest"].map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </Select>
          ) : (
            <Input
              aria-label={`${label}: value`}
              value={a.value === undefined || a.value === null ? "" : String(a.value)}
              type={a.field === "storyPoints" ? "number" : "text"}
              onChange={(e) => onChange({ ...a, value: a.field === "storyPoints" ? (e.target.value === "" ? null : Number(e.target.value)) : e.target.value })}
              placeholder="Value (templates allowed)"
            />
          )}
        </div>
      );
    case "comment":
      return (
        <div>
          <Textarea
            aria-label={`${label}: comment`}
            rows={3}
            value={a.body ?? ""}
            onChange={(e) => onChange({ ...a, body: e.target.value })}
            placeholder="Merged in {{event.url}} — closing {{issue.key}}."
            invalid={!a.body?.trim()}
          />
          <p className="mt-1 flex flex-wrap items-center gap-1 text-xs text-muted">
            Markdown. Insert:
            {templateVars.map((v) => (
              <button key={v} type="button" onClick={() => onChange({ ...a, body: `${a.body ?? ""}${v}` })} className="rounded bg-surface-3 px-1 font-mono text-[11px] text-ink hover:bg-accent-soft">
                {v}
              </button>
            ))}
          </p>
        </div>
      );
    case "add_label":
      return (
        <Input
          aria-label={`${label}: label`}
          value={a.label ?? ""}
          onChange={(e) => onChange({ ...a, label: e.target.value.replace(/\s+/g, "-") })}
          placeholder="needs-triage"
          invalid={!a.label?.trim()}
        />
      );
    case "webhook":
      return (
        <div>
          <Input aria-label={`${label}: URL`} type="url" value={a.url ?? ""} onChange={(e) => onChange({ ...a, url: e.target.value })} placeholder="https://hooks.example.com/automation" />
          <p className="mt-1 text-xs text-muted">Receives a JSON POST with the issue and the event.</p>
        </div>
      );
    case "notify":
      return (
        <Select aria-label={`${label}: recipients`} value={a.recipients ?? "watchers"} onChange={(e) => onChange({ ...a, recipients: e.target.value })} className="max-w-xs">
          <option value="assignee">The assignee</option>
          <option value="reporter">The reporter</option>
          <option value="watchers">Everyone watching</option>
        </Select>
      );
  }
}
