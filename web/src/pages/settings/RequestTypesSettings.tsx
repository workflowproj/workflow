import { useMemo, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Inbox, Pencil, Plus, ShieldCheck, Trash2, X } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { fieldsQuery, issueTypesQuery } from "@/api/queries";
import { Badge, Button, Checkbox, EmptyState, Field, IconButton, Input, Modal, PageHeader, PageSpinner, Select, Textarea, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { UserPicker } from "@/components/UserPicker";
import { cn } from "@/lib/cn";
import { Switch } from "@/features/settings/common";
import { workflowQuery } from "@/features/settings/queries";
import { requestTypesQuery, type RequestType, type RequestTypeInput } from "@/features/servicedesk/api";
import { REQUEST_TYPE_ICONS, RequestTypeIcon } from "@/features/servicedesk/bits";

const route = getRouteApi("/app/projects/$projectKey/settings/request-types");

/** RequestTypesSettings lists a desk's request types: what customers can ask for, and the form for each. */
export function RequestTypesSettings() {
  const { projectKey } = route.useParams();
  const types = useQuery(requestTypesQuery(projectKey));
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [editing, setEditing] = useState<RequestType | "new" | null>(null);
  const list = types.data ?? [];
  const remove = useMutation({
    mutationFn: (t: RequestType) => unwrap(api.DELETE("/request-types/{requestTypeId}", { params: { path: { requestTypeId: t.id } } })),
    onSuccess: (_, t) => {
      qc.invalidateQueries({ queryKey: ["request-types", projectKey] });
      toast.success(`${t.name} deleted`);
    },
    onError: toast.error,
  });
  const groups = [...new Set(list.map((t) => t.group).filter(Boolean))];
  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <PageHeader
        title="Request types"
        subtitle="What people can ask for in the help centre. Each has its own form and, optionally, an approval step."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
            New request type
          </Button>
        }
      />
      {types.error && <ErrorBox error={types.error} />}
      {types.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState icon={<Inbox className="size-8" />} title="No request types" body="Without request types, nobody can raise requests in the help centre." />
        </div>
      ) : (
        <ul className="flex flex-col divide-y divide-line rounded-lg border border-line bg-surface">
          {list.map((t) => (
            <li key={t.id} className="flex items-start gap-3 px-4 py-3">
              <RequestTypeIcon icon={t.icon} />
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium">
                  {t.name}
                  {t.group && <Badge>{t.group}</Badge>}
                  {t.approval && (
                    <Badge tone="warning">
                      <ShieldCheck aria-hidden className="size-3" /> Approval
                    </Badge>
                  )}
                </p>
                {t.description && <p className="mt-0.5 line-clamp-2 text-[13px] text-muted">{t.description}</p>}
                <p className="mt-1 text-xs text-faint">
                  Creates {t.issueType.name} · {t.fields.length} field{t.fields.length === 1 ? "" : "s"} · {t.requestCount} request{t.requestCount === 1 ? "" : "s"}
                </p>
              </div>
              <div className="flex shrink-0">
                <IconButton size="sm" label={`Edit ${t.name}`} onClick={() => setEditing(t)}>
                  <Pencil className="size-4" />
                </IconButton>
                <IconButton
                  size="sm"
                  label={`Delete ${t.name}`}
                  onClick={async () => {
                    if (
                      await confirm({
                        title: `Delete ${t.name}?`,
                        body: t.requestCount
                          ? `Its ${t.requestCount} request${t.requestCount === 1 ? "" : "s"} stay but lose their request type, so customers no longer see them.`
                          : "Customers can no longer choose it.",
                        confirmLabel: "Delete",
                        danger: true,
                      })
                    )
                      remove.mutate(t);
                  }}
                >
                  <Trash2 className="size-4" />
                </IconButton>
              </div>
            </li>
          ))}
        </ul>
      )}
      {editing !== null && (
        <RequestTypeDialog
          key={editing === "new" ? "new" : editing.id}
          projectKey={projectKey}
          type={editing === "new" ? null : editing}
          groups={groups}
          nextPosition={(list.at(-1)?.position ?? 0) + 10}
          onClose={() => setEditing(null)}
        />
      )}
    </div>
  );
}

type Condition = { field: string; values: string[] };
type FormRow = { field: string; label: string; help: string; required: boolean; showWhen?: Condition };
type FormChoice = { key: string; name: string; type: string; options?: string[] };

const PRIORITIES = ["highest", "high", "medium", "low", "lowest"];
const CHOICE_TYPES = new Set(["select", "multiselect", "checkbox"]);

/** choicesOf lists the answers a condition can test: select options, priorities, or ticked/unticked. */
function choicesOf(f: FormChoice): { value: string; label: string }[] {
  if (f.type === "checkbox") return [{ value: "true", label: "Ticked" }, { value: "false", label: "Not ticked" }];
  if (f.key === "priority") return PRIORITIES.map((p) => ({ value: p, label: p.charAt(0).toUpperCase() + p.slice(1) }));
  return (f.options ?? []).map((o) => ({ value: o, label: o }));
}
type ApprovalState = {
  on: boolean;
  mode: "any" | "all";
  approvers: { id: string; displayName: string; avatarUrl?: string }[];
  groupId: string;
  approvedStatus: string;
  declinedStatus: string;
};

const SYSTEM_FIELDS = [
  { key: "summary", name: "Summary", type: "text" },
  { key: "description", name: "Description", type: "textarea" },
  { key: "priority", name: "Priority", type: "select" },
  { key: "dueDate", name: "Due date", type: "date" },
];
// Custom field types a form can ask for; user and version fields would show customers the project's people and releases.
const FORM_TYPES = new Set(["text", "url", "number", "select", "multiselect", "date", "checkbox"]);

/** RequestTypeDialog is the form builder: the request type's details, its ordered form fields and its approval. */
function RequestTypeDialog({
  projectKey,
  type,
  groups,
  nextPosition,
  onClose,
}: {
  projectKey: string;
  type: RequestType | null;
  groups: string[];
  nextPosition: number;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: issueTypes = [] } = useQuery(issueTypesQuery(projectKey));
  const { data: defs = [] } = useQuery(fieldsQuery);
  const { data: allGroups = [] } = useQuery({ queryKey: ["groups", ""], queryFn: () => unwrap(api.GET("/groups")) });
  const standard = issueTypes.filter((t) => t.level === 0 && t.kind === "work");
  const [name, setName] = useState(type?.name ?? "");
  const [description, setDescription] = useState(type?.description ?? "");
  const [icon, setIcon] = useState(type?.icon ?? "help");
  const [issueTypeKey, setIssueTypeKey] = useState(type?.issueType.key ?? "");
  const [group, setGroup] = useState(type?.group ?? "");
  const [position, setPosition] = useState(String(type?.position ?? nextPosition));
  const [rows, setRows] = useState<FormRow[]>(
    type?.fields.map((f) => ({ field: f.field, label: f.label, help: f.help ?? "", required: f.required, showWhen: f.showWhen })) ?? [
      { field: "summary", label: "Summary", help: "", required: true },
      { field: "description", label: "Description", help: "", required: false },
    ],
  );
  const [approval, setApproval] = useState<ApprovalState>({
    on: !!type?.approval,
    mode: type?.approval?.mode ?? "any",
    approvers: type?.approval?.approvers ?? [],
    groupId: type?.approval?.groupId ?? "",
    approvedStatus: type?.approval?.approvedStatus ?? "Waiting for support",
    declinedStatus: type?.approval?.declinedStatus ?? "Declined",
  });
  const typeKey = issueTypeKey || standard[0]?.key || "";
  const issueType = issueTypes.find((t) => t.key === typeKey);
  const workflow = useQuery({ ...workflowQuery(issueType?.workflowId ?? ""), enabled: !!issueType });
  const statuses = workflow.data?.version.definition.statuses ?? [];

  // The fields this issue type has that a form may ask for.
  const available = useMemo(() => {
    const custom = defs
      .filter((d) => FORM_TYPES.has(d.type) && (!d.projectKeys || d.projectKeys.includes(projectKey)) && (!d.issueTypeKeys || d.issueTypeKeys.includes(typeKey)))
      .map((d): FormChoice => ({ key: d.key, name: d.name, type: d.type, options: d.options }));
    return [...SYSTEM_FIELDS, ...custom] as FormChoice[];
  }, [defs, projectKey, typeKey]);
  const nameOf = (key: string) => available.find((f) => f.key === key);
  const unused = available.filter((f) => !rows.some((r) => r.field === f.key));

  const save = useMutation({
    mutationFn: () => {
      const body: RequestTypeInput = {
        name: name.trim(),
        description: description.trim(),
        icon,
        issueTypeKey: typeKey,
        group: group.trim(),
        position: Number(position) || 0,
        fields: rows.map((r) => ({
          field: r.field,
          label: r.label.trim() || (nameOf(r.field)?.name ?? r.field),
          help: r.help.trim() || undefined,
          required: r.required,
          showWhen: r.showWhen?.values.length ? r.showWhen : undefined,
        })),
      };
      if (approval.on) {
        body.approval = {
          mode: approval.mode,
          approverIds: approval.approvers.map((a) => a.id),
          groupId: approval.groupId || undefined,
          approvedStatus: approval.approvedStatus,
          declinedStatus: approval.declinedStatus,
        };
      }
      return type
        ? unwrap(api.PUT("/request-types/{requestTypeId}", { params: { path: { requestTypeId: type.id } }, body }))
        : unwrap(api.POST("/projects/{projectKey}/request-types", { params: { path: { projectKey } }, body }));
    },
    onSuccess: (t) => {
      qc.invalidateQueries({ queryKey: ["request-types", projectKey] });
      qc.invalidateQueries({ queryKey: ["portal"] });
      toast.success(type ? `${t.name} saved` : `${t.name} added`);
      onClose();
    },
  });
  const errs = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const update = (i: number, patch: Partial<FormRow>) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const moveRow = (i: number, d: number) =>
    setRows((rs) => {
      const next = [...rs];
      [next[i], next[i + d]] = [next[i + d], next[i]];
      return next;
    });

  return (
    <Modal
      open
      onClose={onClose}
      size="xl"
      title={type ? `Edit ${type.name}` : "New request type"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="request-type-form" loading={save.isPending} disabled={!name.trim() || !typeKey}>
            {type ? "Save" : "Add request type"}
          </Button>
        </>
      }
    >
      <form
        id="request-type-form"
        className="flex flex-col gap-6"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="Name" required error={errs.name}>
            {(id) => <Input id={id} value={name} maxLength={120} onChange={(e) => setName(e.target.value)} autoFocus required placeholder="e.g. Request new software" />}
          </Field>
          <Field label="Creates" hint="The issue type agents work on." error={errs.issueTypeKey}>
            {(id) => (
              <Select id={id} value={typeKey} onChange={(e) => setIssueTypeKey(e.target.value)}>
                {standard.map((t) => (
                  <option key={t.key} value={t.key}>
                    {t.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Description" hint="Helps people pick the right request type." className="md:col-span-2" error={errs.description}>
            {(id) => <Textarea id={id} rows={2} value={description} maxLength={2000} onChange={(e) => setDescription(e.target.value)} />}
          </Field>
          <Field label="Group" hint="Request types with the same group are listed together." error={errs.group}>
            {(id) => (
              <>
                <Input id={id} list="request-type-groups" value={group} maxLength={60} onChange={(e) => setGroup(e.target.value)} placeholder="None" />
                <datalist id="request-type-groups">
                  {groups.map((g) => (
                    <option key={g} value={g} />
                  ))}
                </datalist>
              </>
            )}
          </Field>
          <Field label="Order" hint="Lower numbers come first." error={errs.position}>
            {(id) => <Input id={id} type="number" value={position} onChange={(e) => setPosition(e.target.value)} />}
          </Field>
        </div>

        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium">Icon</legend>
          <div className="flex flex-wrap gap-2">
            {REQUEST_TYPE_ICONS.map((k) => (
              <label key={k} className={cn("cursor-pointer rounded-lg border-2 p-0.5", icon === k ? "border-accent" : "border-transparent hover:border-line-strong")}>
                <input type="radio" name="request-type-icon" value={k} checked={icon === k} onChange={() => setIcon(k)} className="sr-only" aria-label={k} />
                <RequestTypeIcon icon={k} />
              </label>
            ))}
          </div>
        </fieldset>

        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 text-sm font-semibold">Form</legend>
          <p className="-mt-1 text-[13px] text-muted">What the customer fills in, in this order. Without a summary field, the request type's name becomes the summary.</p>
          {errs.fields && <p className="text-xs text-danger">{errs.fields}</p>}
          <ol className="flex flex-col gap-2">
            {rows.map((r, i) => {
              const f = nameOf(r.field);
              const where = `fields[${i}]`;
              return (
                <li key={r.field} className="rounded-md border border-line bg-surface-2/40 p-3">
                  <div className="mb-2 flex items-center gap-2">
                    <span className="text-[13px] font-medium">{f?.name ?? r.field}</span>
                    <span className="text-xs text-faint">{f?.type ?? "unavailable"}</span>
                    <span className="ml-auto flex">
                      <IconButton size="sm" label={`Move ${f?.name ?? r.field} up`} disabled={i === 0} onClick={() => moveRow(i, -1)}>
                        <ArrowUp className="size-4" />
                      </IconButton>
                      <IconButton size="sm" label={`Move ${f?.name ?? r.field} down`} disabled={i === rows.length - 1} onClick={() => moveRow(i, 1)}>
                        <ArrowDown className="size-4" />
                      </IconButton>
                      <IconButton
                        size="sm"
                        label={`Remove ${f?.name ?? r.field}`}
                        // Conditions on the removed field go with it.
                        onClick={() => setRows((rs) => rs.filter((_, j) => j !== i).map((x) => (x.showWhen?.field === r.field ? { ...x, showWhen: undefined } : x)))}
                      >
                        <X className="size-4" />
                      </IconButton>
                    </span>
                  </div>
                  <div className="grid gap-2 md:grid-cols-[1fr_1.4fr_auto] md:items-center">
                    <Input
                      aria-label={`Label for ${f?.name ?? r.field}`}
                      value={r.label}
                      placeholder={f?.name ?? "Label"}
                      maxLength={120}
                      onChange={(e) => update(i, { label: e.target.value })}
                    />
                    <Input
                      aria-label={`Help for ${f?.name ?? r.field}`}
                      value={r.help}
                      placeholder="Help text (optional)"
                      maxLength={500}
                      onChange={(e) => update(i, { help: e.target.value })}
                    />
                    <Checkbox label="Required" checked={r.required} onChange={(e) => update(i, { required: e.target.checked })} />
                  </div>
                  {r.field !== "summary" && (
                    <ShowWhen
                      name={f?.name ?? r.field}
                      value={r.showWhen}
                      // Only choice fields earlier in the form can be tested.
                      earlier={rows
                        .slice(0, i)
                        .map((x) => nameOf(x.field))
                        .filter((x): x is FormChoice => !!x && (CHOICE_TYPES.has(x.type) || x.key === "priority"))}
                      later={!!r.showWhen && rows.slice(i + 1).some((x) => x.field === r.showWhen?.field)}
                      onChange={(showWhen) => update(i, { showWhen })}
                    />
                  )}
                  {(errs[where] || errs[`${where}.showWhen`]) && <p className="mt-1 text-xs text-danger">{errs[where] ?? errs[`${where}.showWhen`]}</p>}
                </li>
              );
            })}
          </ol>
          {unused.length > 0 && (
            <div className="flex items-center gap-2">
              <label htmlFor="add-form-field" className="sr-only">
                Add a field
              </label>
              <Select
                id="add-form-field"
                className="max-w-xs"
                value=""
                onChange={(e) => {
                  const f = available.find((x) => x.key === e.target.value);
                  if (f) setRows((rs) => [...rs, { field: f.key, label: f.name, help: "", required: false }]);
                }}
              >
                <option value="">Add a field…</option>
                {unused.map((f) => (
                  <option key={f.key} value={f.key}>
                    {f.name}
                  </option>
                ))}
              </Select>
            </div>
          )}
        </fieldset>

        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-sm font-semibold">Approval</legend>
          <Switch checked={approval.on} onChange={(on) => setApproval((a) => ({ ...a, on }))} label="Ask for approval before the team starts work" />
          {approval.on && (
            <div className="flex flex-col gap-4 rounded-md border border-line p-3">
              <p className="text-[13px] text-muted">
                New requests wait in <strong className="text-ink">Pending approval</strong>; approvers decide in the help centre or the issue. One decline declines the request.
              </p>
              <div className="flex flex-col gap-1.5">
                <span className="text-[13px] font-medium">Approvers</span>
                {approval.approvers.length > 0 && (
                  <ul className="flex flex-wrap gap-1.5">
                    {approval.approvers.map((p) => (
                      <li key={p.id} className="inline-flex items-center gap-1.5 rounded-full border border-line bg-surface py-0.5 pr-1 pl-0.5 text-[13px]">
                        <UserAvatar user={p} size={20} />
                        {p.displayName}
                        <IconButton
                          size="sm"
                          className="size-5"
                          label={`Remove ${p.displayName}`}
                          onClick={() => setApproval((a) => ({ ...a, approvers: a.approvers.filter((x) => x.id !== p.id) }))}
                        >
                          <X className="size-3" />
                        </IconButton>
                      </li>
                    ))}
                  </ul>
                )}
                <div className="max-w-xs">
                  <UserPicker
                    label="Add an approver"
                    noneLabel="Add an approver…"
                    allowNone={false}
                    value={null}
                    onChange={(u) => u && setApproval((a) => (a.approvers.some((x) => x.id === u.id) ? a : { ...a, approvers: [...a.approvers, u] }))}
                  />
                </div>
                {(errs["approval.approverIds"] || errs.approval) && <p className="text-xs text-danger">{errs["approval.approverIds"] ?? errs.approval}</p>}
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <Field label="And the members of" hint="Members when the request is raised." error={errs["approval.groupId"]}>
                  {(id) => (
                    <Select id={id} value={approval.groupId} onChange={(e) => setApproval((a) => ({ ...a, groupId: e.target.value }))}>
                      <option value="">No group</option>
                      {allGroups.map((g) => (
                        <option key={g.id} value={g.id}>
                          {g.displayName}
                        </option>
                      ))}
                    </Select>
                  )}
                </Field>
                <Field label="Approved when">
                  {(id) => (
                    <Select id={id} value={approval.mode} onChange={(e) => setApproval((a) => ({ ...a, mode: e.target.value as "any" | "all" }))}>
                      <option value="any">Any one approver approves</option>
                      <option value="all">Every approver approves</option>
                    </Select>
                  )}
                </Field>
                <StatusSelect
                  label="When approved, move to"
                  value={approval.approvedStatus}
                  statuses={statuses}
                  error={errs["approval.approvedStatus"]}
                  onChange={(v) => setApproval((a) => ({ ...a, approvedStatus: v }))}
                />
                <StatusSelect
                  label="When declined, move to"
                  value={approval.declinedStatus}
                  statuses={statuses}
                  error={errs["approval.declinedStatus"]}
                  onChange={(v) => setApproval((a) => ({ ...a, declinedStatus: v }))}
                />
              </div>
            </div>
          )}
        </fieldset>
        {save.error && !Object.keys(errs).length ? <ErrorBox error={save.error} /> : null}
      </form>
    </Modal>
  );
}

/** ShowWhen edits a form field's condition: always shown, or only when an earlier choice field has one of the picked answers. */
function ShowWhen({
  name,
  value,
  earlier,
  later,
  onChange,
}: {
  name: string;
  value?: Condition;
  earlier: FormChoice[];
  later: boolean;
  onChange: (c: Condition | undefined) => void;
}) {
  const ctl = earlier.find((x) => x.key === value?.field);
  if (earlier.length === 0 && !value) return null;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[13px]">
      <label className="flex items-center gap-2">
        <span className="text-muted">Show</span>
        <Select
          aria-label={`When to show ${name}`}
          className="w-auto"
          value={value?.field ?? ""}
          onChange={(e) => onChange(e.target.value ? { field: e.target.value, values: [] } : undefined)}
        >
          <option value="">Always</option>
          {earlier.map((x) => (
            <option key={x.key} value={x.key}>
              When {x.name} is…
            </option>
          ))}
        </Select>
      </label>
      {ctl &&
        choicesOf(ctl).map((c) => (
          <Checkbox
            key={c.value}
            label={c.label}
            checked={value!.values.includes(c.value)}
            onChange={(e) => onChange({ field: ctl.key, values: e.target.checked ? [...value!.values, c.value] : value!.values.filter((v) => v !== c.value) })}
          />
        ))}
      {ctl && value!.values.length === 0 && <span className="text-xs text-danger">Pick at least one answer</span>}
      {later && <span className="text-xs text-danger">Move this below the field it depends on</span>}
    </div>
  );
}

function StatusSelect({
  label,
  value,
  statuses,
  error,
  onChange,
}: {
  label: string;
  value: string;
  statuses: Schemas["WorkflowStatusDef"][];
  error?: string;
  onChange: (v: string) => void;
}) {
  const names = statuses.map((s) => s.name).filter((n) => n !== "Pending approval");
  return (
    <Field label={label} error={error}>
      {(id) => (
        <Select id={id} value={value} onChange={(e) => onChange(e.target.value)}>
          {!names.includes(value) && <option value={value}>{value}</option>}
          {names.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </Select>
      )}
    </Field>
  );
}
