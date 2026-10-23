import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Plus, X } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { projectsQuery } from "@/api/queries";
import { Button, Checkbox, Field, IconButton, Input, Modal, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";

type FieldDef = Schemas["FieldDefinition"];
type FieldType = Schemas["FieldType"];

export const FIELD_TYPES: { id: FieldType; label: string }[] = [
  { id: "text", label: "Text" },
  { id: "number", label: "Number" },
  { id: "select", label: "Select (one option)" },
  { id: "multiselect", label: "Multi-select" },
  { id: "user", label: "Person" },
  { id: "date", label: "Date" },
  { id: "version", label: "Release" },
  { id: "checkbox", label: "Checkbox" },
  { id: "url", label: "URL" },
];
const COMMON_TYPES = ["epic", "story", "task", "bug", "subtask"];

const keyFrom = (name: string) =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^[^a-z]+|_+$/g, "")
    .slice(0, 41);

export function FieldDialog({ field, onClose }: { field?: FieldDef; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: projects = [] } = useQuery(projectsQuery);
  const [name, setName] = useState(field?.name ?? "");
  const [key, setKey] = useState(field?.key ?? "");
  const [keyTouched, setKeyTouched] = useState(!!field);
  const [description, setDescription] = useState(field?.description ?? "");
  const [type, setType] = useState<FieldType>(field?.type ?? "text");
  const [options, setOptions] = useState<string[]>(field?.options.length ? field.options : ["", ""]);
  const [projectKeys, setProjectKeys] = useState<string[]>(field?.projectKeys ?? []);
  const [typeKeys, setTypeKeys] = useState<string[]>(field?.issueTypeKeys ?? []);
  const [typeDraft, setTypeDraft] = useState("");
  const hasOptions = type === "select" || type === "multiselect";

  const save = useMutation({
    mutationFn: () => {
      const body: Schemas["FieldDefinitionInput"] = {
        key,
        name: name.trim(),
        description,
        type,
        options: hasOptions ? options.map((o) => o.trim()).filter(Boolean) : [],
        projectKeys,
        issueTypeKeys: typeKeys,
      };
      return field ? unwrap(api.PUT("/fields/{fieldId}", { params: { path: { fieldId: field.id } }, body })) : unwrap(api.POST("/fields", { body }));
    },
    onSuccess: (f) => {
      qc.invalidateQueries({ queryKey: ["fields"] });
      qc.invalidateQueries({ queryKey: ["search-fields"] });
      toast.success(field ? `Saved ${f.name}` : `Created ${f.name}`);
      onClose();
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const dupe = save.error instanceof ApiError && save.error.code === "duplicate_field" ? save.error.message : undefined;
  const addType = (k: string) => {
    const v = k.trim().toLowerCase();
    if (v && !typeKeys.includes(v)) setTypeKeys([...typeKeys, v]);
    setTypeDraft("");
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={field ? `Edit ${field.name}` : "New custom field"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="field-form" loading={save.isPending} disabled={!name.trim() || !key || (hasOptions && !options.some((o) => o.trim()))}>
            {field ? "Save field" : "Create field"}
          </Button>
        </>
      }
    >
      <form
        id="field-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="grid gap-4 sm:grid-cols-[1fr_13rem]">
          <Field label="Name" required error={fe.name}>
            {(id) => (
              <Input
                id={id}
                autoFocus
                value={name}
                maxLength={80}
                onChange={(e) => {
                  setName(e.target.value);
                  if (!keyTouched) setKey(keyFrom(e.target.value));
                }}
                placeholder="Customer impact"
              />
            )}
          </Field>
          <Field label="Key" required error={(field ? undefined : fe.key) ?? dupe} hint={field ? "Fixed once created." : "Used in JQL, CEL (issue.fields.key) and the API."}>
            {(id) => (
              <Input
                id={id}
                value={key}
                disabled={!!field}
                maxLength={41}
                className="font-mono"
                onChange={(e) => {
                  setKeyTouched(true);
                  setKey(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ""));
                }}
              />
            )}
          </Field>
        </div>
        <Field label="Description" hint="Shown as help text next to the field.">
          {(id) => <Textarea id={id} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}
        </Field>
        <Field label="Type" error={fe.type ?? (field ? fe.key : undefined)} hint={field ? "The type cannot change once created." : undefined}>
          {(id) => (
            <Select id={id} value={type} disabled={!!field} onChange={(e) => setType(e.target.value as FieldType)} className="max-w-64">
              {FIELD_TYPES.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.label}
                </option>
              ))}
            </Select>
          )}
        </Field>
        {hasOptions && (
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Options</legend>
            <ol className="flex flex-col gap-1.5">
              {options.map((o, i) => (
                <li key={i} className="flex items-center gap-1.5">
                  <span className="w-5 text-right text-xs text-faint">{i + 1}.</span>
                  <Input
                    aria-label={`Option ${i + 1}`}
                    value={o}
                    onChange={(e) => setOptions(options.map((x, k) => (k === i ? e.target.value : x)))}
                    className="!h-8"
                    placeholder={i === 0 ? "High" : "Low"}
                  />
                  <IconButton
                    label={`Move option ${i + 1} up`}
                    size="sm"
                    disabled={i === 0}
                    onClick={() => {
                      const next = [...options];
                      [next[i - 1], next[i]] = [next[i], next[i - 1]];
                      setOptions(next);
                    }}
                  >
                    <ArrowUp className="size-3.5" />
                  </IconButton>
                  <IconButton
                    label={`Move option ${i + 1} down`}
                    size="sm"
                    disabled={i === options.length - 1}
                    onClick={() => {
                      const next = [...options];
                      [next[i + 1], next[i]] = [next[i], next[i + 1]];
                      setOptions(next);
                    }}
                  >
                    <ArrowDown className="size-3.5" />
                  </IconButton>
                  <IconButton label={`Remove option ${i + 1}`} size="sm" disabled={options.length === 1} onClick={() => setOptions(options.filter((_, k) => k !== i))}>
                    <X className="size-3.5" />
                  </IconButton>
                </li>
              ))}
            </ol>
            <Button size="sm" variant="ghost" className="mt-1.5" icon={<Plus className="size-3.5" />} onClick={() => setOptions([...options, ""])}>
              Add option
            </Button>
            {fe.options && <p className="mt-1 text-xs text-danger">{fe.options}</p>}
          </fieldset>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Projects</legend>
            <Checkbox label="All projects" checked={projectKeys.length === 0} onChange={(e) => setProjectKeys(e.target.checked ? [] : projects.slice(0, 1).map((p) => p.key))} />
            {projectKeys.length > 0 && (
              <div className="mt-2 flex max-h-40 flex-col gap-1 overflow-y-auto rounded-md border border-line p-2">
                {projects.map((p) => (
                  <Checkbox
                    key={p.key}
                    label={
                      <span>
                        {p.name} <span className="font-mono text-xs text-faint">{p.key}</span>
                      </span>
                    }
                    checked={projectKeys.includes(p.key)}
                    onChange={(e) => setProjectKeys(e.target.checked ? [...projectKeys, p.key] : projectKeys.filter((k) => k !== p.key))}
                  />
                ))}
              </div>
            )}
            {fe.projectKeys && <p className="mt-1 text-xs text-danger">{fe.projectKeys}</p>}
          </fieldset>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Issue types</legend>
            <Checkbox label="All issue types" checked={typeKeys.length === 0} onChange={(e) => setTypeKeys(e.target.checked ? [] : ["story"])} />
            {typeKeys.length > 0 && (
              <div className="mt-2 rounded-md border border-line p-2">
                <div className="flex flex-wrap gap-1">
                  {typeKeys.map((k) => (
                    <span key={k} className="inline-flex items-center gap-1 rounded bg-surface-2 py-0.5 pr-0.5 pl-1.5 font-mono text-xs">
                      {k}
                      <button type="button" aria-label={`Remove ${k}`} onClick={() => setTypeKeys(typeKeys.filter((x) => x !== k))} className="rounded p-0.5 text-faint hover:text-ink">
                        <X className="size-3" />
                      </button>
                    </span>
                  ))}
                </div>
                <Input
                  aria-label="Add an issue type key"
                  value={typeDraft}
                  onChange={(e) => setTypeDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === ",") {
                      e.preventDefault();
                      addType(typeDraft);
                    }
                  }}
                  placeholder="Type key, then Enter"
                  className="mt-2 !h-7.5 font-mono text-xs"
                />
                <div className="mt-1.5 flex flex-wrap gap-1">
                  {COMMON_TYPES.filter((k) => !typeKeys.includes(k)).map((k) => (
                    <button
                      key={k}
                      type="button"
                      onClick={() => addType(k)}
                      className={cn("rounded border border-dashed border-line px-1.5 py-0.5 font-mono text-[11px] text-muted hover:border-accent hover:text-accent")}
                    >
                      + {k}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </fieldset>
        </div>
        {save.error && !dupe && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}
