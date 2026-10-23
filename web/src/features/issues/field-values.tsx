import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import type { Schemas } from "@/api/client";
import { useUsers } from "@/api/queries";
import { PRIORITIES, type User } from "@/api/types";
import { Checkbox, Input, Select } from "@/components/ui";
import { UserPicker } from "@/components/UserPicker";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { PriorityIcon, priorityLabel } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import { releasesQuery } from "./issue-api";
import { InlineDate, InlineMulti, InlineSelect, InlineText, InlineUser, LabelsEditor } from "./inline-edit";

export type FieldDef = Schemas["FieldDefinition"];

/** fieldsFor keeps the custom fields that apply to an issue type in a project. */
export function fieldsFor(defs: FieldDef[], projectKey: string, typeKey: string) {
  return defs.filter((d) => (!d.projectKeys || d.projectKeys.includes(projectKey)) && (!d.issueTypeKeys || d.issueTypeKeys.includes(typeKey)));
}

/** A value as the API stores it: string, number, boolean or string list. */
export type FieldValue = unknown;

const asString = (v: FieldValue) => (typeof v === "string" ? v : typeof v === "number" ? String(v) : "");
const asList = (v: FieldValue) => (Array.isArray(v) ? (v as string[]) : []);

/** useUserById resolves a user id from the project's assignable people. */
function useUserById(id: string, project: string): User | undefined {
  const { data = [] } = useUsers("", project);
  return id ? data.find((u) => u.id === id) : undefined;
}

/**
 * CustomFieldInline renders a custom field in the details sidebar and edits it in place.
 * `onCommit(null)` clears the field.
 */
export function CustomFieldInline({ def, value, onCommit, disabled, projectKey }: { def: FieldDef; value: FieldValue; onCommit: (v: FieldValue | null) => void; disabled: boolean; projectKey: string }) {
  const label = def.name;
  switch (def.type) {
    case "number":
      return <InlineText type="number" label={label} value={asString(value)} disabled={disabled} onCommit={(v) => onCommit(v === "" ? null : Number(v))} />;
    case "date":
      return <InlineDate label={label} value={asString(value) || null} disabled={disabled} onCommit={onCommit} />;
    case "url":
      return (
        <InlineText type="url" label={label} value={asString(value)} disabled={disabled} onCommit={(v) => onCommit(v || null)}>
          <span className="truncate text-accent">{asString(value).replace(/^https?:\/\//, "")}</span>
          <a
            href={asString(value)}
            target="_blank"
            rel="noreferrer noopener"
            aria-label={`Open ${label}`}
            onClick={(e) => e.stopPropagation()}
            className="ml-auto shrink-0 rounded p-0.5 text-faint hover:text-accent"
          >
            <ExternalLink className="size-3.5" />
          </a>
        </InlineText>
      );
    case "select":
      return <InlineSelect label={label} value={asString(value) || null} allowNone options={def.options.map((o) => ({ value: o, label: o }))} disabled={disabled} onCommit={onCommit} />;
    case "multiselect":
      return <InlineMulti label={label} values={asList(value)} options={def.options.map((o) => ({ value: o, label: o }))} disabled={disabled} onCommit={(v) => onCommit(v.length ? v : null)} />;
    case "checkbox":
      return (
        <div className="flex min-h-8 items-center">
          <Checkbox label={value === true ? "Yes" : "No"} checked={value === true} disabled={disabled} onChange={(e) => onCommit(e.target.checked)} />
        </div>
      );
    case "user":
      return <UserField id={asString(value)} label={label} projectKey={projectKey} disabled={disabled} onCommit={(u) => onCommit(u?.id ?? null)} />;
    case "version":
      return <VersionField id={asString(value)} label={label} projectKey={projectKey} disabled={disabled} onCommit={onCommit} />;
    default:
      return <InlineText label={label} value={asString(value)} disabled={disabled} onCommit={(v) => onCommit(v || null)} />;
  }
}

function UserField({ id, label, projectKey, disabled, onCommit }: { id: string; label: string; projectKey: string; disabled: boolean; onCommit: (u: User | null) => void }) {
  const user = useUserById(id, projectKey);
  const shown = user ?? (id ? ({ id, displayName: "Unknown user", email: "", kind: "human" } as User) : null);
  return <InlineUser label={label} value={shown} project={projectKey} disabled={disabled} noneLabel="None" onCommit={onCommit} />;
}

function VersionField({ id, label, projectKey, disabled, onCommit }: { id: string; label: string; projectKey: string; disabled: boolean; onCommit: (v: string | null) => void }) {
  const { data: releases = [] } = useQuery(releasesQuery(projectKey));
  return <InlineSelect label={label} value={id || null} allowNone options={releases.map((r) => ({ value: r.id, label: r.name, hint: r.state === "released" ? "released" : undefined }))} disabled={disabled} onCommit={onCommit} />;
}

// ---------------------------------------------------------------------------
// Form controls (transition screens)

export type ScreenField = Schemas["ScreenFieldInfo"];

/** isEmptyValue mirrors the server's notion of a missing required value. */
export function isEmptyValue(v: unknown) {
  return v === undefined || v === null || (typeof v === "string" && v.trim() === "") || (Array.isArray(v) && v.length === 0);
}

/**
 * ScreenFieldControl renders one transition-screen field as a form control. User fields hold a
 * User object (sent as its id); everything else holds the API value.
 */
export function ScreenFieldControl({
  field,
  value,
  onChange,
  projectKey,
  id,
  invalid,
}: {
  field: ScreenField;
  value: unknown;
  onChange: (v: unknown) => void;
  projectKey: string;
  id: string;
  invalid?: boolean;
}): ReactNode {
  const options = field.options ?? [];
  switch (field.type) {
    case "markdown":
      return <MarkdownEditor value={asString(value)} onChange={onChange} placeholder="Write a comment…" minHeight={80} className={cn(invalid && "border-danger")} />;
    case "select":
      return (
        <Select id={id} value={asString(value)} invalid={invalid} onChange={(e) => onChange(e.target.value || undefined)}>
          <option value="">{field.required ? "Choose…" : "None"}</option>
          {options.map((o) => (
            <option key={o} value={o}>
              {field.field === "priority" && PRIORITIES.includes(o as never) ? priorityLabel(o as never) : o}
            </option>
          ))}
        </Select>
      );
    case "multiselect":
      return (
        <div className="flex flex-wrap gap-x-4 gap-y-2 rounded-md border border-line p-2.5" id={id}>
          {options.map((o) => (
            <Checkbox key={o} label={o} checked={asList(value).includes(o)} onChange={(e) => onChange(e.target.checked ? [...asList(value), o] : asList(value).filter((x) => x !== o))} />
          ))}
        </div>
      );
    case "checkbox":
      return <Checkbox id={id} label="Yes" checked={value === true} onChange={(e) => onChange(e.target.checked)} />;
    case "user":
      return <UserPicker value={(value as User | null) ?? null} onChange={onChange} project={projectKey} label={field.name} />;
    case "number":
      return <Input id={id} type="number" step="any" invalid={invalid} value={asString(value)} onChange={(e) => onChange(e.target.value)} className="max-w-40" />;
    case "date":
      return <Input id={id} type="date" invalid={invalid} value={asString(value)} onChange={(e) => onChange(e.target.value || undefined)} className="max-w-48" />;
    case "labels":
      return <LabelsEditor id={id} value={asList(value)} onChange={onChange} projectKey={projectKey} />;
    case "versions":
      return <ReleaseChecklist id={id} projectKey={projectKey} value={asList(value)} onChange={onChange} />;
    case "version":
      return <ReleaseSelect id={id} projectKey={projectKey} value={asString(value)} onChange={onChange} invalid={invalid} />;
    default:
      return <Input id={id} type={field.type === "url" ? "url" : "text"} invalid={invalid} value={asString(value)} onChange={(e) => onChange(e.target.value)} placeholder={field.type === "url" ? "https://" : undefined} />;
  }
}

function ReleaseChecklist({ id, projectKey, value, onChange }: { id: string; projectKey: string; value: string[]; onChange: (v: string[]) => void }) {
  const { data: releases = [] } = useQuery(releasesQuery(projectKey));
  const shown = releases.filter((r) => r.state === "unreleased" || value.includes(r.id));
  if (shown.length === 0) return <p className="text-sm text-muted">No unreleased versions in this project.</p>;
  return (
    <div id={id} className="flex flex-wrap gap-x-4 gap-y-2 rounded-md border border-line p-2.5">
      {shown.map((r) => (
        <Checkbox key={r.id} label={r.name} checked={value.includes(r.id)} onChange={(e) => onChange(e.target.checked ? [...value, r.id] : value.filter((x) => x !== r.id))} />
      ))}
    </div>
  );
}

function ReleaseSelect({ id, projectKey, value, onChange, invalid }: { id: string; projectKey: string; value: string; onChange: (v: string | undefined) => void; invalid?: boolean }) {
  const { data: releases = [] } = useQuery(releasesQuery(projectKey));
  return (
    <Select id={id} value={value} invalid={invalid} onChange={(e) => onChange(e.target.value || undefined)}>
      <option value="">None</option>
      {releases.map((r) => (
        <option key={r.id} value={r.id}>
          {r.name}
        </option>
      ))}
    </Select>
  );
}

/** Priority options with icons for menus. */
export const priorityOptions = PRIORITIES.map((p) => ({ value: p, label: priorityLabel(p), icon: <PriorityIcon priority={p} /> }));
