import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import type { IssueType } from "@/api/types";
import { Button, Field, Input, Modal, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import { Callout } from "./common";
import { workflowQuery, workflowsQuery } from "./queries";
import { StatusMappingFields, suggestMapping, type RemovedStatus } from "./StatusMapping";

export const ICONS = [
  { id: "story", label: "Story" },
  { id: "task", label: "Task" },
  { id: "bug", label: "Bug" },
  { id: "epic", label: "Epic" },
  { id: "subtask", label: "Sub-task" },
];
const COLORS = ["#22a06b", "#1d7afc", "#e2483d", "#8b5cf6", "#5e8fff", "#f59e0b", "#0d9488", "#db2777", "#64748b"];
export const LEVELS = [
  { value: 2, label: "Initiative", hint: "Above epics" },
  { value: 1, label: "Epic", hint: "Groups standard issues" },
  { value: 0, label: "Standard", hint: "Stories, tasks, bugs" },
  { value: -1, label: "Sub-task", hint: "Belongs to a standard issue" },
];

function keyFrom(name: string) {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/^[^a-z]+/, "")
    .slice(0, 31);
}

/** IssueTypeDialog creates or edits an issue type; changing its workflow may need a status mapping. */
export function IssueTypeDialog({ open, onClose, projectKey, type }: { open: boolean; onClose: () => void; projectKey: string; type?: IssueType }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: workflows = [] } = useQuery(workflowsQuery(projectKey));
  const [name, setName] = useState(type?.name ?? "");
  const [key, setKey] = useState(type?.key ?? "");
  const [keyTouched, setKeyTouched] = useState(!!type);
  const [description, setDescription] = useState(type?.description ?? "");
  const [icon, setIcon] = useState(type?.icon ?? "task");
  const [color, setColor] = useState(type?.color ?? COLORS[1]);
  const [level, setLevel] = useState(type?.level ?? 0);
  const [workflowId, setWorkflowId] = useState(type?.workflowId ?? "");
  const [missing, setMissing] = useState<RemovedStatus[]>([]);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const wfId = workflowId || workflows[0]?.id || "";
  const target = useQuery({ ...workflowQuery(wfId), enabled: open && missing.length > 0 && !!wfId });
  const targets = target.data?.version.definition.statuses ?? [];
  const { data: statuses = [] } = useQuery({ queryKey: ["statuses"], queryFn: () => unwrap(api.GET("/statuses")), enabled: missing.length > 0 });

  const save = useMutation({
    mutationFn: async () => {
      if (!type) {
        const body: Schemas["IssueTypeInput"] = { key, name: name.trim(), description, icon, color, level, workflowId: wfId };
        return unwrap(api.POST("/projects/{projectKey}/issue-types", { params: { path: { projectKey } }, body }));
      }
      const body: Record<string, unknown> = {};
      if (name.trim() !== type.name) body.name = name.trim();
      if (description !== type.description) body.description = description;
      if (icon !== type.icon) body.icon = icon;
      if (color !== type.color) body.color = color;
      if (wfId !== type.workflowId) {
        body.workflowId = wfId;
        if (missing.length) body.statusMapping = mapping;
      }
      return unwrap(api.PATCH("/projects/{projectKey}/issue-types/{typeId}", { params: { path: { projectKey, typeId: type.id } }, body }));
    },
    onSuccess: (t) => {
      for (const k of ["issue-types", "workflows", "board", "backlog", "search"]) qc.invalidateQueries({ queryKey: [k] });
      toast.success(type ? (type.workflowId !== t.workflowId ? `${t.name} now uses ${t.workflowName}; its issues are being migrated.` : `Saved ${t.name}`) : `Created ${t.name}`);
      onClose();
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "status_mapping_required") {
        const removed = (e.problem.errors ?? []).filter((f) => f.field).map((f) => ({ name: f.field!, message: f.message }));
        setMissing(removed);
      }
    },
  });
  const err = save.error;
  const mappingNeeded = err instanceof ApiError && err.code === "status_mapping_required";
  const fe = err instanceof ApiError && !mappingNeeded ? err.fieldErrors() : {};
  const keyError = fe.key ?? (err instanceof ApiError && err.code === "duplicate_type" ? err.message : undefined);
  const complete = missing.every((m) => mapping[m.name]);
  const categoryOf = (n: string) => statuses.find((s) => s.name.toLowerCase() === n.toLowerCase())?.category;

  // Seed the mapping once the new workflow's statuses are known.
  useEffect(() => {
    if (missing.length && targets.length) setMapping((m) => (Object.keys(m).length ? m : suggestMapping(missing, targets, categoryOf)));
  }, [missing, targets.length, statuses.length]);

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={type ? `Edit ${type.name}` : "New issue type"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="issue-type-form" loading={save.isPending} disabled={!name.trim() || !key || !wfId || (missing.length > 0 && !complete)}>
            {missing.length ? "Change workflow and migrate" : type ? "Save" : "Create"}
          </Button>
        </>
      }
    >
      <form
        id="issue-type-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="flex items-center gap-3 rounded-md border border-line bg-surface-2 px-3 py-2.5">
          <IssueTypeIcon type={{ icon, color, name: name || "Preview" }} className="!size-6 rounded [&_svg]:size-4" />
          <div>
            <p className="text-sm font-medium">{name || "Issue type"}</p>
            <p className="text-xs text-muted">
              {LEVELS.find((l) => l.value === level)?.label} · keys look like {projectKey}-42
            </p>
          </div>
        </div>
        <div className="grid gap-4 sm:grid-cols-[1fr_12rem]">
          <Field label="Name" required error={fe.name}>
            {(id) => (
              <Input
                id={id}
                autoFocus
                value={name}
                maxLength={60}
                onChange={(e) => {
                  setName(e.target.value);
                  if (!keyTouched) setKey(keyFrom(e.target.value));
                }}
                placeholder="Spike"
              />
            )}
          </Field>
          <Field label="Key" required error={keyError} hint={type ? "Keys can't change; JQL uses them (type = spike)." : "Used in JQL and the API."}>
            {(id) => (
              <Input
                id={id}
                value={key}
                disabled={!!type}
                maxLength={31}
                className="font-mono"
                invalid={!!keyError}
                onChange={(e) => {
                  setKeyTouched(true);
                  setKey(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ""));
                }}
              />
            )}
          </Field>
        </div>
        <Field label="Description">{(id) => <Textarea id={id} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}</Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Icon</legend>
            <div className="flex flex-wrap gap-1.5">
              {ICONS.map((i) => (
                <button
                  key={i.id}
                  type="button"
                  aria-pressed={icon === i.id}
                  aria-label={i.label}
                  title={i.label}
                  onClick={() => setIcon(i.id)}
                  className={cn("rounded-md border p-1.5", icon === i.id ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
                >
                  <IssueTypeIcon type={{ icon: i.id, color, name: i.label }} className="!size-5 [&_svg]:size-3.5" />
                </button>
              ))}
            </div>
          </fieldset>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Colour</legend>
            <div className="flex flex-wrap items-center gap-1.5">
              {COLORS.map((c) => (
                <button
                  key={c}
                  type="button"
                  aria-pressed={color === c}
                  aria-label={`Colour ${c}`}
                  onClick={() => setColor(c)}
                  style={{ background: c }}
                  className={cn("size-6 rounded-full border-2", color === c ? "border-ink" : "border-transparent")}
                />
              ))}
              <input type="color" aria-label="Custom colour" value={color} onChange={(e) => setColor(e.target.value)} className="size-7 cursor-pointer rounded border border-line bg-surface" />
            </div>
          </fieldset>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Hierarchy level" hint={type ? "The level is fixed once issues can exist." : LEVELS.find((l) => l.value === level)?.hint}>
            {(id) => (
              <Select id={id} value={level} disabled={!!type} onChange={(e) => setLevel(Number(e.target.value))}>
                {LEVELS.map((l) => (
                  <option key={l.value} value={l.value}>
                    {l.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Workflow" error={fe.workflowId} hint={type && wfId !== type.workflowId ? "Existing issues move to the new workflow in the background." : undefined}>
            {(id) => (
              <Select
                id={id}
                value={wfId}
                onChange={(e) => {
                  setWorkflowId(e.target.value);
                  setMissing([]);
                  setMapping({});
                  save.reset();
                }}
              >
                {workflows.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name}
                    {w.shared ? " (shared)" : ""}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        {missing.length > 0 && (
          <div className="flex flex-col gap-2">
            <Callout tone="warning" title={`Some ${type?.name ?? ""} issues are in statuses ${target.data?.name ?? "the new workflow"} doesn't have`}>
              Choose where they should go; the change is applied together with the move.
            </Callout>
            <StatusMappingFields removed={missing} targets={targets} value={mapping} onChange={setMapping} oldCategory={categoryOf} />
          </div>
        )}
        {err && !mappingNeeded && Object.keys(fe).length === 0 && !keyError && <ErrorBox error={err} />}
      </form>
    </Modal>
  );
}
