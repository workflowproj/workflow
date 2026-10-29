import { useState } from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Send, ShieldCheck } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, Checkbox, Input, PageSpinner, Select, Textarea } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { RequestTypeIcon } from "@/features/servicedesk/bits";
import { portalDeskQuery, type PortalFormField } from "@/features/servicedesk/api";
import { Crumbs } from "./parts";

const route = getRouteApi("/portal/$projectKey/new/$requestTypeId");

type Values = Record<string, unknown>;

const optionLabel = (field: PortalFormField, o: string) => (field.key === "priority" ? o.charAt(0).toUpperCase() + o.slice(1) : o);

export function isBlank(v: unknown) {
  return v === undefined || v === null || (typeof v === "string" && v.trim() === "") || (Array.isArray(v) && v.length === 0);
}

/** visibleFields mirrors the server: a field with showWhen is shown while the earlier field it
 * tests is shown and answered with one of its values (any of them for a multiselect; an unticked
 * checkbox is "false"). */
export function visibleFields(fields: PortalFormField[], values: Values): Set<string> {
  const shown = new Set<string>();
  const types = new Map<string, PortalFormField["type"]>();
  for (const f of fields) {
    types.set(f.key, f.type);
    const c = f.showWhen;
    if (!c) {
      shown.add(f.key);
      continue;
    }
    let v = values[c.field];
    if (types.get(c.field) === "checkbox") v = String(!!v);
    const answers = Array.isArray(v) ? v : [v];
    if (shown.has(c.field) && answers.some((a) => typeof a === "string" && c.values.includes(a))) shown.add(f.key);
  }
  return shown;
}

/** formValues prepares the form for the API: hidden fields and blanks are left out, numbers are numbers. */
export function formValues(fields: PortalFormField[], values: Values): Values {
  const out: Values = {};
  const shown = visibleFields(fields, values);
  for (const f of fields) {
    if (!shown.has(f.key)) continue;
    const v = values[f.key];
    if (f.type === "checkbox") {
      if (v) out[f.key] = true;
      continue;
    }
    if (isBlank(v)) continue;
    out[f.key] = f.type === "number" ? Number(v) : typeof v === "string" ? v.trim() : v;
  }
  return out;
}

/** PortalCreatePage is a request type's form. */
export function PortalCreatePage() {
  const { projectKey, requestTypeId } = route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: desk, isLoading, error } = useQuery(portalDeskQuery(projectKey));
  const [values, setValues] = useState<Values>({});
  const [missing, setMissing] = useState<Record<string, string>>({});
  const type = desk?.requestTypes.find((t) => t.id === requestTypeId);
  const create = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/portal/projects/{projectKey}/requests", { params: { path: { projectKey } }, body: { requestTypeId, values: formValues(type!.fields, values) } })),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["portal"] });
      navigate({ to: "/portal/requests/$issueKey", params: { issueKey: r.key }, search: { created: true } });
    },
  });
  if (isLoading) return <PageSpinner label="Loading the form" />;
  if (error || !desk) return <ErrorBox error={error ?? new Error("Help desk not found")} />;
  if (!type) return <ErrorBox error={new Error("This request type no longer exists.")} />;

  const serverErrors = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  const errorFor = (key: string) => missing[key] ?? serverErrors[`values.${key}`];
  const set = (key: string, v: unknown) => {
    setValues((cur) => ({ ...cur, [key]: v }));
    setMissing(({ [key]: _, ...rest }) => rest);
  };
  const shown = visibleFields(type.fields, values);
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    for (const f of type.fields) if (shown.has(f.key) && f.required && f.type !== "checkbox" && isBlank(values[f.key])) errs[f.key] = `${f.label} is required`;
    setMissing(errs);
    if (Object.keys(errs).length === 0) create.mutate();
    else document.getElementById(`field-${Object.keys(errs)[0]}`)?.focus();
  };
  const fieldErrorsShown = Object.keys(serverErrors).some((k) => k.startsWith("values."));

  return (
    <div className="max-w-2xl">
      <Crumbs items={[{ label: "Help centre", to: "/portal" }, { label: desk.name, to: "/portal/$projectKey", params: { projectKey: desk.projectKey } }, { label: type.name }]} />
      <div className="flex items-start gap-3">
        <RequestTypeIcon icon={type.icon} size="lg" />
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight">{type.name}</h1>
          {type.description && <p className="mt-1 text-sm text-muted">{type.description}</p>}
        </div>
      </div>
      {type.needsApproval && (
        <p className="mt-4 flex items-start gap-2 rounded-md border border-line bg-surface-2 px-3 py-2 text-sm text-muted">
          <ShieldCheck aria-hidden className="mt-0.5 size-4 shrink-0 text-accent" />
          This request needs approval before the team starts on it. We'll ask the approvers as soon as you send it.
        </p>
      )}
      <form onSubmit={submit} noValidate className="mt-6 flex flex-col gap-5 rounded-lg border border-line bg-surface p-5">
        {create.error && !fieldErrorsShown ? <ErrorBox error={create.error} /> : null}
        {type.fields
          .filter((f) => shown.has(f.key))
          .map((f) => (
            <FormField key={f.key} field={f} value={values[f.key]} error={errorFor(f.key)} onChange={(v) => set(f.key, v)} />
          ))}
        {type.fields.length === 0 && <p className="text-sm text-muted">Nothing else to fill in: send the request and the team will be in touch.</p>}
        <div className="flex flex-wrap items-center gap-2 border-t border-line pt-4">
          <Button type="submit" variant="primary" loading={create.isPending} icon={<Send className="size-4" />}>
            Send request
          </Button>
          <Link to="/portal/$projectKey" params={{ projectKey: desk.projectKey }} className="rounded-md px-3 py-1.5 text-sm text-muted hover:bg-surface-2 hover:text-ink">
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}

/** FormField renders one form field by its type. */
function FormField({ field: f, value, error, onChange }: { field: PortalFormField; value: unknown; error?: string; onChange: (v: unknown) => void }) {
  const id = `field-${f.key}`;
  const described = error ? `${id}-error` : f.help ? `${id}-help` : undefined;
  if (f.type === "checkbox" || f.type === "multiselect") {
    const selected = Array.isArray(value) ? (value as string[]) : [];
    return (
      <fieldset aria-describedby={described} className="flex flex-col gap-1.5">
        <legend className="mb-1 text-[13px] font-medium">
          {f.label}
          {f.required && f.type === "multiselect" && <span className="ml-0.5 text-danger">*</span>}
        </legend>
        {f.type === "checkbox" ? (
          <Checkbox id={id} label={f.help || "Yes"} checked={!!value} onChange={(e) => onChange(e.target.checked)} />
        ) : (
          <div className="flex flex-col gap-1.5">
            {(f.options ?? []).map((o, i) => (
              <Checkbox
                key={o}
                id={i === 0 ? id : undefined}
                label={o}
                checked={selected.includes(o)}
                onChange={(e) => onChange(e.target.checked ? [...selected, o] : selected.filter((x) => x !== o))}
              />
            ))}
          </div>
        )}
        {error ? (
          <p id={`${id}-error`} className="text-xs text-danger">
            {error}
          </p>
        ) : f.help && f.type !== "checkbox" ? (
          <p id={`${id}-help`} className="text-xs text-muted">
            {f.help}
          </p>
        ) : null}
      </fieldset>
    );
  }
  const common = {
    id,
    invalid: !!error,
    "aria-invalid": !!error || undefined,
    "aria-describedby": described,
    "aria-required": f.required || undefined,
  };
  const str = typeof value === "string" ? value : value === undefined || value === null ? "" : String(value);
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-[13px] font-medium text-ink">
        {f.label}
        {f.required && <span className="ml-0.5 text-danger">*</span>}
      </label>
      {f.type === "textarea" ? (
        <Textarea {...common} rows={6} value={str} onChange={(e) => onChange(e.target.value)} />
      ) : f.type === "select" ? (
        <Select {...common} value={str} onChange={(e) => onChange(e.target.value)}>
          <option value="">{f.required ? "Choose…" : "None"}</option>
          {(f.options ?? []).map((o) => (
            <option key={o} value={o}>
              {optionLabel(f, o)}
            </option>
          ))}
        </Select>
      ) : (
        <Input
          {...common}
          type={f.type === "number" ? "number" : f.type === "date" ? "date" : f.type === "url" ? "url" : "text"}
          value={str}
          maxLength={f.key === "summary" ? 255 : undefined}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {error ? (
        <p id={`${id}-error`} className="text-xs text-danger">
          {error}
        </p>
      ) : f.help ? (
        <p id={`${id}-help`} className="text-xs text-muted">
          {f.help}
        </p>
      ) : null}
    </div>
  );
}
