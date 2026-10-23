import { useId } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Trash2, X } from "lucide-react";
import { fieldsQuery } from "@/api/queries";
import { Button, Checkbox, IconButton, Input, Select, Textarea } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import { CelHelp, type CelExample } from "@/features/settings/CelHelp";
import { POST_FUNCTION_KINDS, postFunctionKind, slug, updateTransition, type PostFunctionDoc, type PostFunctionKind, type TransitionDoc, type WorkflowDoc } from "./model";
import { errorAt, FieldError, PanelSection, RowCard, type PathError } from "./panel-bits";
import type { WorkflowEditor } from "./useWorkflowEditor";

export const SYSTEM_SCREEN_FIELDS = ["resolution", "comment", "assignee", "priority", "storyPoints", "dueDate", "labels", "fixVersions"];

const conditionExamples: CelExample[] = [
  { label: "Only the assignee", expr: "issue.assignee == actor.email" },
  { label: "Project admins only", expr: 'actor.hasRole("admin")' },
  { label: "People who can edit anyone's issues", expr: 'actor.hasPermission("issue.edit_others")' },
  { label: "Only bugs", expr: 'issue.type == "bug"' },
];
const validatorExamples: CelExample[] = [
  { label: "All sub-tasks are done", expr: 'issue.subtasks.all(s, s.category == "done")' },
  { label: "Someone is assigned", expr: 'issue.assignee != ""' },
  { label: "Has a merged pull request", expr: 'issue.devLinks.exists(l, l.kind == "pr" && l.merged)' },
  { label: "Estimated", expr: "issue.storyPoints > 0" },
];

const pfLabels: Record<PostFunctionKind, string> = {
  assignTo: "Assign the issue",
  setField: "Set a field",
  clearField: "Clear a field",
  notify: "Notify people",
  webhook: "Call a webhook",
};

function emptyPostFunction(kind: PostFunctionKind): PostFunctionDoc {
  switch (kind) {
    case "assignTo":
      return { assignTo: "actor" };
    case "setField":
      return { setField: { field: "", value: "" } };
    case "clearField":
      return { clearField: "" };
    case "notify":
      return { notify: "watchers" };
    case "webhook":
      return { webhook: "https://" };
  }
}

export function TransitionPanel({ doc, index, apply, onDelete, errors }: { doc: WorkflowDoc; index: number; apply?: WorkflowEditor["apply"]; onDelete: () => void; errors: PathError[] }) {
  const t = doc.transitions[index];
  const { data: fields = [] } = useQuery(fieldsQuery);
  const ids = { name: useId(), id: useId(), to: useId(), cond: useId(), fieldList: useId() };
  if (!t) return null;
  const readOnly = !apply;
  const prefix = `transitions[${index}]`;
  const set = (patch: Partial<TransitionDoc>, key?: string) => apply?.((d) => updateTransition(d, index, patch), { key: key ? `t${index}.${key}` : undefined });
  const global = !t.from;
  const sources = doc.statuses.filter((s) => s.name.toLowerCase() !== t.to.toLowerCase());
  const fieldOptions = [...SYSTEM_SCREEN_FIELDS, ...fields.map((f) => f.key)];
  const derivedId = slug(t.name);

  return (
    <div>
      <PanelSection title="Transition">
        <div className="flex flex-col gap-3">
          <div>
            <label htmlFor={ids.name} className="mb-1 block text-[13px] font-medium">
              Name
            </label>
            <Input id={ids.name} value={t.name} disabled={readOnly} invalid={!!errorAt(errors, prefix, ".name")} onChange={(e) => set({ name: e.target.value }, "name")} />
            <FieldError message={errorAt(errors, prefix, ".name")} />
          </div>
          <div>
            <label htmlFor={ids.id} className="mb-1 block text-[13px] font-medium">
              ID <span className="font-normal text-muted">(optional)</span>
            </label>
            <Input
              id={ids.id}
              value={t.id ?? ""}
              placeholder={derivedId}
              disabled={readOnly}
              className="font-mono text-[13px]"
              invalid={!!errorAt(errors, prefix, ".id")}
              onChange={(e) => set({ id: e.target.value || undefined }, "id")}
            />
            <FieldError message={errorAt(errors, prefix, ".id")} />
            <p className="mt-1 text-xs text-muted">
              Used by the API and smart commits (<code className="font-mono">#{t.id || derivedId || "id"}</code>). Pin it to keep integrations working when you rename the transition.
            </p>
          </div>
        </div>
      </PanelSection>

      <PanelSection title="From → to">
        <Checkbox
          label="Available from any status (global)"
          checked={global}
          disabled={readOnly}
          onChange={(e) => set({ from: e.target.checked ? undefined : sources.slice(0, 1).map((s) => s.name) })}
        />
        {!global && (
          <fieldset className="mt-2">
            <legend className="sr-only">From statuses</legend>
            <div className="flex flex-col gap-1 rounded-md border border-line p-2">
              {sources.map((s) => {
                const on = t.from?.some((f) => f.toLowerCase() === s.name.toLowerCase()) ?? false;
                return (
                  <label key={s.name} className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 hover:bg-surface-2">
                    <input
                      type="checkbox"
                      className="size-4 accent-accent"
                      checked={on}
                      disabled={readOnly || (on && t.from!.length === 1)}
                      onChange={(e) => set({ from: e.target.checked ? [...(t.from ?? []), s.name] : t.from!.filter((f) => f.toLowerCase() !== s.name.toLowerCase()) })}
                    />
                    <StatusBadge status={s} />
                  </label>
                );
              })}
            </div>
            <FieldError message={errorAt(errors, prefix, ".from")} />
          </fieldset>
        )}
        <label htmlFor={ids.to} className="mt-3 mb-1 block text-[13px] font-medium">
          To
        </label>
        <Select
          id={ids.to}
          value={doc.statuses.find((s) => s.name.toLowerCase() === t.to.toLowerCase())?.name ?? t.to}
          disabled={readOnly}
          onChange={(e) => set({ to: e.target.value, from: t.from?.filter((f) => f.toLowerCase() !== e.target.value.toLowerCase()) })}
        >
          {!doc.statuses.some((s) => s.name.toLowerCase() === t.to.toLowerCase()) && <option value={t.to}>{t.to} (unknown)</option>}
          {doc.statuses.map((s) => (
            <option key={s.name} value={s.name}>
              {s.name}
            </option>
          ))}
        </Select>
        <FieldError message={errorAt(errors, prefix, ".to")} />
      </PanelSection>

      <PanelSection title="Condition" hint="Who can see and run this transition. Leave empty to allow anyone with the transition permission.">
        <Textarea
          id={ids.cond}
          aria-label="Condition (CEL)"
          rows={2}
          spellCheck={false}
          value={t.condition ?? ""}
          disabled={readOnly}
          placeholder='actor.hasRole("admin")'
          invalid={!!errorAt(errors, prefix, ".condition")}
          className="font-mono text-[12px]"
          onChange={(e) => set({ condition: e.target.value }, "condition")}
        />
        <FieldError message={errorAt(errors, prefix, ".condition")} />
        <CelHelp className="mt-2" context="transition" examples={conditionExamples} onInsert={readOnly ? undefined : (expr) => set({ condition: expr })} />
      </PanelSection>

      <PanelSection
        title={`Validators (${t.validators?.length ?? 0})`}
        hint="Checked when the transition runs; the message is shown when one fails."
        action={!readOnly && <AddButton label="Add validator" onClick={() => set({ validators: [...(t.validators ?? []), { expr: "", message: "" }] })} />}
      >
        <div className="flex flex-col gap-2">
          {(t.validators ?? []).map((v, j) => (
            <RowCard key={j}>
              <RemoveButton label="Remove validator" hidden={readOnly} onClick={() => set({ validators: t.validators!.filter((_, k) => k !== j) })} />
              <Textarea
                aria-label={`Validator ${j + 1} expression`}
                rows={2}
                spellCheck={false}
                value={v.expr}
                disabled={readOnly}
                placeholder='issue.assignee != ""'
                className="pr-8 font-mono text-[12px]"
                invalid={!!errorAt(errors, prefix, `.validators[${j}]`)}
                onChange={(e) => set({ validators: t.validators!.map((x, k) => (k === j ? { ...x, expr: e.target.value } : x)) }, `v${j}.expr`)}
              />
              <Input
                aria-label={`Validator ${j + 1} message`}
                value={v.message ?? ""}
                disabled={readOnly}
                placeholder="Message when it fails"
                onChange={(e) => set({ validators: t.validators!.map((x, k) => (k === j ? { ...x, message: e.target.value || undefined } : x)) }, `v${j}.msg`)}
              />
              <FieldError message={errorAt(errors, prefix, `.validators[${j}]`)} />
            </RowCard>
          ))}
          {(t.validators?.length ?? 0) > 0 && (
            <CelHelp
              context="transition"
              examples={validatorExamples}
              onInsert={readOnly ? undefined : (expr) => set({ validators: [...(t.validators ?? []).filter((x) => x.expr.trim()), { expr }] })}
            />
          )}
        </div>
      </PanelSection>

      <PanelSection
        title={`Screen (${t.screen?.length ?? 0})`}
        hint="Fields people fill in when running the transition, e.g. a resolution or a comment."
        action={
          !readOnly && <AddButton label="Add field" onClick={() => set({ screen: [...(t.screen ?? []), { field: fieldOptions.find((f) => !t.screen?.some((s) => s.field === f)) ?? "comment" }] })} />
        }
      >
        <datalist id={ids.fieldList}>
          {fieldOptions.map((f) => (
            <option key={f} value={f} />
          ))}
        </datalist>
        <div className="flex flex-col gap-1.5">
          {(t.screen ?? []).map((f, j) => (
            <div key={j}>
              <div className="flex items-center gap-2">
                <Input
                  aria-label={`Screen field ${j + 1}`}
                  list={ids.fieldList}
                  value={f.field}
                  disabled={readOnly}
                  className="!h-7.5 flex-1 font-mono text-[12px]"
                  invalid={!!errorAt(errors, prefix, `.screen[${j}]`)}
                  onChange={(e) => set({ screen: t.screen!.map((x, k) => (k === j ? { ...x, field: e.target.value } : x)) }, `s${j}`)}
                />
                <Checkbox
                  label="Required"
                  checked={!!f.required}
                  disabled={readOnly}
                  onChange={(e) => set({ screen: t.screen!.map((x, k) => (k === j ? { field: x.field, ...(e.target.checked ? { required: true } : {}) } : x)) })}
                />
                {!readOnly && (
                  <IconButton label="Remove field" size="sm" onClick={() => set({ screen: t.screen!.filter((_, k) => k !== j) })}>
                    <X className="size-3.5" />
                  </IconButton>
                )}
              </div>
              <FieldError message={errorAt(errors, prefix, `.screen[${j}]`)} />
            </div>
          ))}
        </div>
      </PanelSection>

      <PanelSection
        title={`Post-functions (${t.postFunctions?.length ?? 0})`}
        hint="Run in order after the transition succeeds."
        action={
          !readOnly && (
            <Select
              aria-label="Add post-function"
              value=""
              className="!h-7 !w-auto py-0 pr-7 text-xs"
              onChange={(e) => e.target.value && set({ postFunctions: [...(t.postFunctions ?? []), emptyPostFunction(e.target.value as PostFunctionKind)] })}
            >
              <option value="">+ Add…</option>
              {POST_FUNCTION_KINDS.map((k) => (
                <option key={k} value={k}>
                  {pfLabels[k]}
                </option>
              ))}
            </Select>
          )
        }
      >
        <div className="flex flex-col gap-2">
          {(t.postFunctions ?? []).map((pf, j) => (
            <PostFunctionRow
              key={j}
              pf={pf}
              index={j}
              readOnly={readOnly}
              fieldListId={ids.fieldList}
              error={errorAt(errors, prefix, `.postFunctions[${j}]`)}
              onChange={(next) => set({ postFunctions: t.postFunctions!.map((x, k) => (k === j ? next : x)) }, `pf${j}`)}
              onRemove={() => set({ postFunctions: t.postFunctions!.filter((_, k) => k !== j) })}
            />
          ))}
        </div>
      </PanelSection>

      {!readOnly && (
        <PanelSection title="Danger zone">
          <Button size="sm" icon={<Trash2 className="size-3.5 text-danger" />} onClick={onDelete}>
            Delete transition
          </Button>
        </PanelSection>
      )}
    </div>
  );
}

function PostFunctionRow({
  pf,
  index,
  readOnly,
  fieldListId,
  error,
  onChange,
  onRemove,
}: {
  pf: PostFunctionDoc;
  index: number;
  readOnly: boolean;
  fieldListId: string;
  error?: string;
  onChange: (pf: PostFunctionDoc) => void;
  onRemove: () => void;
}) {
  const kind = postFunctionKind(pf) ?? "assignTo";
  const label = `Post-function ${index + 1}`;
  const assignPreset = ["actor", "reporter", "unassigned"].includes(pf.assignTo ?? "") ? pf.assignTo! : "email";
  return (
    <RowCard>
      <RemoveButton label="Remove post-function" hidden={readOnly} onClick={onRemove} />
      <p className="pr-8 text-xs font-semibold text-muted">
        {index + 1}. {pfLabels[kind]}
      </p>
      {kind === "assignTo" && (
        <>
          <Select aria-label={`${label}: assignee`} value={assignPreset} disabled={readOnly} onChange={(e) => onChange({ assignTo: e.target.value === "email" ? "" : e.target.value })}>
            <option value="actor">The person running the transition</option>
            <option value="reporter">The reporter</option>
            <option value="unassigned">Nobody (unassign)</option>
            <option value="email">A specific person (email)…</option>
          </Select>
          {assignPreset === "email" && (
            <Input aria-label={`${label}: email`} type="email" placeholder="lead@example.com" value={pf.assignTo ?? ""} disabled={readOnly} onChange={(e) => onChange({ assignTo: e.target.value })} />
          )}
        </>
      )}
      {kind === "setField" && (
        <div className="grid grid-cols-2 gap-2">
          <Input
            aria-label={`${label}: field`}
            list={fieldListId}
            placeholder="field"
            className="font-mono text-[12px]"
            value={pf.setField?.field ?? ""}
            disabled={readOnly}
            onChange={(e) => onChange({ setField: { ...pf.setField, field: e.target.value } })}
          />
          <Input
            aria-label={`${label}: value`}
            placeholder="value"
            value={pf.setField?.value === undefined || pf.setField?.value === null ? "" : String(pf.setField.value)}
            disabled={readOnly}
            onChange={(e) => {
              const raw = e.target.value;
              const value = /^-?\d+(\.\d+)?$/.test(raw) ? Number(raw) : raw === "true" ? true : raw === "false" ? false : raw;
              onChange({ setField: { field: pf.setField?.field ?? "", value } });
            }}
          />
        </div>
      )}
      {kind === "clearField" && (
        <Input
          aria-label={`${label}: field`}
          list={fieldListId}
          placeholder="field"
          className="font-mono text-[12px]"
          value={pf.clearField ?? ""}
          disabled={readOnly}
          onChange={(e) => onChange({ clearField: e.target.value })}
        />
      )}
      {kind === "notify" && (
        <Select aria-label={`${label}: recipients`} value={pf.notify} disabled={readOnly} onChange={(e) => onChange({ notify: e.target.value })}>
          <option value="assignee">The assignee</option>
          <option value="reporter">The reporter</option>
          <option value="watchers">Everyone watching</option>
        </Select>
      )}
      {kind === "webhook" && (
        <>
          <Input
            aria-label={`${label}: URL`}
            type="url"
            placeholder="https://hooks.example.com/…"
            value={pf.webhook ?? ""}
            disabled={readOnly}
            onChange={(e) => onChange({ webhook: e.target.value })}
          />
          <p className="text-xs text-muted">Receives a signed JSON POST (X-WFM-Signature-256) after the transition.</p>
        </>
      )}
      <FieldError message={error} />
    </RowCard>
  );
}

function AddButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button size="sm" variant="ghost" className="h-6 px-1.5 text-xs" icon={<Plus className="size-3.5" />} onClick={onClick} aria-label={label}>
      Add
    </Button>
  );
}

function RemoveButton({ label, onClick, hidden }: { label: string; onClick: () => void; hidden?: boolean }) {
  if (hidden) return null;
  return (
    <IconButton label={label} size="sm" className="absolute top-1.5 right-1.5 size-6" onClick={onClick}>
      <X className="size-3.5" />
    </IconButton>
  );
}
