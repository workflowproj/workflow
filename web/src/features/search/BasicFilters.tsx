import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Search, X } from "lucide-react";
import { issueTypesQuery, labelsQuery, projectsQuery, statusesQuery, useUsers } from "@/api/queries";
import { PRIORITIES } from "@/api/types";
import { Popover } from "@/components/ui";
import { IssueTypeIcon, PriorityIcon, priorityLabel } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { ProjectAvatar } from "@/layout/AppShell";
import { useDebounced } from "@/features/issues/IssuePicker";
import { MenuKeys } from "@/features/issues/inline-edit";
import { cn } from "@/lib/cn";
import { basicIsEmpty, emptyBasic, type BasicField, type BasicFilters as Filters } from "./jql";

type Option = { value: string; label: string; icon?: ReactNode; hint?: string };

const categoryDot = { todo: "bg-todo", in_progress: "bg-progress", done: "bg-done" } as const;

/**
 * BasicFilters edits a query as chips (project, type, status, assignee, priority, labels and
 * text). Every change is reported straight away; the navigator turns it into JQL.
 */
export function BasicFilters({ value, onChange, projectKey }: { value: Filters; onChange: (f: Filters) => void; projectKey?: string }) {
  const set = (field: BasicField) => (v: string[]) => onChange({ ...value, [field]: v });
  const { data: projects = [] } = useQuery(projectsQuery);
  const { data: statuses = [] } = useQuery(statusesQuery);
  const scope = projectKey ? [projectKey] : value.project.length ? value.project : projects.map((p) => p.key);
  const types = useQueries({ queries: scope.map((k) => issueTypesQuery(k)) });
  const labels = useQueries({ queries: scope.map((k) => labelsQuery(k)) });

  const typeOptions = useMemo(() => {
    const seen = new Map<string, Option>();
    for (const q of types) for (const t of q.data ?? []) if (!seen.has(t.name)) seen.set(t.name, { value: t.name, label: t.name, icon: <IssueTypeIcon type={t} /> });
    return [...seen.values()];
  }, [types]);
  const labelOptions = useMemo(() => [...new Set(labels.flatMap((q) => q.data ?? []))].sort().map((l) => ({ value: l, label: l })), [labels]);
  const statusOptions = useMemo(() => {
    const seen = new Map<string, Option>();
    for (const s of statuses)
      if (!seen.has(s.name)) seen.set(s.name, { value: s.name, label: s.name, icon: <span className={cn("size-2 shrink-0 rounded-full", categoryDot[s.category])} /> });
    return [...seen.values()];
  }, [statuses]);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <TextFilter value={value.text} onChange={(text) => onChange({ ...value, text })} />
      {!projectKey && (
        <FilterChip
          label="Project"
          selected={value.project}
          onChange={set("project")}
          options={projects.map((p) => ({ value: p.key, label: p.name, hint: p.key, icon: <ProjectAvatar projectKey={p.key} size={16} /> }))}
        />
      )}
      <FilterChip label="Type" selected={value.type} onChange={set("type")} options={typeOptions} />
      <FilterChip label="Status" selected={value.status} onChange={set("status")} options={statusOptions} searchable />
      <AssigneeChip selected={value.assignee} onChange={set("assignee")} projectKey={projectKey} />
      <FilterChip
        label="Priority"
        selected={value.priority}
        onChange={set("priority")}
        options={PRIORITIES.map((p) => ({ value: priorityLabel(p), label: priorityLabel(p), icon: <PriorityIcon priority={p} /> }))}
      />
      <FilterChip label="Labels" selected={value.labels} onChange={set("labels")} options={labelOptions} searchable empty="No labels in use yet" />
      {!basicIsEmpty(value) && (
        <button type="button" onClick={() => onChange(emptyBasic())} className="h-8 rounded-md px-2 text-[13px] text-muted hover:bg-surface-2 hover:text-ink">
          Clear filters
        </button>
      )}
    </div>
  );
}

function TextFilter({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [text, setText] = useState(value);
  const debounced = useDebounced(text, 400);
  useEffect(() => setText(value), [value]);
  useEffect(() => {
    if (debounced !== value) onChange(debounced);
    // Only react to typing, not to the value coming back from the URL.
  }, [debounced]);
  return (
    <div className="relative w-48">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint" />
      <input
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onChange(text);
          if (e.key === "Escape" && text) {
            setText("");
            onChange("");
          }
        }}
        placeholder="Search text"
        aria-label="Search issue text"
        className="h-8 w-full rounded-md border border-line-strong bg-surface pr-7 pl-8 text-sm placeholder:text-faint focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none"
      />
      {text && (
        <button
          type="button"
          aria-label="Clear text"
          onClick={() => (setText(""), onChange(""))}
          className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded p-0.5 text-faint hover:text-ink"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}

function AssigneeChip({ selected, onChange, projectKey }: { selected: string[]; onChange: (v: string[]) => void; projectKey?: string }) {
  const [q, setQ] = useState("");
  const { data: users = [] } = useUsers(q, projectKey);
  const options: Option[] = [
    { value: "currentUser()", label: "Me", icon: <UserAvatar user={{ displayName: "Me" }} size={18} /> },
    { value: "EMPTY", label: "Unassigned", icon: <UserAvatar user={null} size={18} /> },
    ...users.map((u) => ({ value: u.email, label: u.displayName, hint: u.email, icon: <UserAvatar user={u} size={18} /> })),
  ];
  return <FilterChip label="Assignee" selected={selected} onChange={onChange} options={options} search={q} onSearch={setQ} />;
}

/** FilterChip is a multi-select button; its menu filters options as you type. */
function FilterChip({
  label,
  selected,
  onChange,
  options,
  searchable,
  search,
  onSearch,
  empty = "No options",
}: {
  label: string;
  selected: string[];
  onChange: (v: string[]) => void;
  options: Option[];
  searchable?: boolean;
  /** Remote search: the parent filters `options` from `search`. */
  search?: string;
  onSearch?: (q: string) => void;
  empty?: string;
}) {
  const [local, setLocal] = useState("");
  const q = onSearch ? (search ?? "") : local;
  const shownOptions = onSearch ? options : options.filter((o) => `${o.label} ${o.hint ?? ""}`.toLowerCase().includes(local.toLowerCase()));
  // Keep chosen values visible even when the current search does not return them.
  const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();
  const isOn = (v: string) => selected.some((s) => same(s, v));
  const missing: Option[] = selected.filter((v) => !options.some((o) => same(o.value, v))).map((v) => ({ value: v, label: v }));
  const all = [...missing, ...shownOptions];
  const labelFor = (v: string) => options.find((o) => same(o.value, v))?.label ?? v;
  const active = selected.length > 0;
  return (
    <Popover
      trigger={({ toggle, open }) => (
        <button
          type="button"
          onClick={toggle}
          aria-haspopup="menu"
          aria-expanded={open}
          className={cn(
            "inline-flex h-8 max-w-64 items-center gap-1.5 rounded-md border px-2.5 text-[13px] transition-colors",
            active ? "border-accent/40 bg-accent-soft text-accent" : "border-line-strong bg-surface text-ink hover:bg-surface-2",
          )}
        >
          <span className={cn(active && "font-medium")}>{label}</span>
          {active && (
            <span className="truncate">
              : {labelFor(selected[0])}
              {selected.length > 1 && ` +${selected.length - 1}`}
            </span>
          )}
          <ChevronDown className="size-3.5 shrink-0 opacity-60" />
        </button>
      )}
      content={(close) => (
        <MenuKeys close={close} className="w-64">
          {(searchable || onSearch || options.length > 8) && (
            <input
              value={q}
              onChange={(e) => (onSearch ? onSearch(e.target.value) : setLocal(e.target.value))}
              placeholder={`Find ${label.toLowerCase()}…`}
              aria-label={`Find ${label.toLowerCase()}`}
              className="mb-1 h-8 w-full rounded border border-line bg-surface px-2 text-sm focus:border-accent focus:outline-none"
            />
          )}
          <div className="max-h-72 overflow-y-auto">
            {all.map((o) => {
              const on = isOn(o.value);
              return (
                <button
                  key={o.value}
                  type="button"
                  role="menuitemcheckbox"
                  aria-checked={on}
                  onClick={() => onChange(on ? selected.filter((v) => !same(v, o.value)) : [...selected, o.value])}
                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-surface-2 focus:bg-surface-2 focus:outline-none"
                >
                  <span className={cn("flex size-4 shrink-0 items-center justify-center rounded border", on ? "border-accent bg-accent text-accent-ink" : "border-line-strong")}>
                    {on && <Check className="size-3" strokeWidth={3} />}
                  </span>
                  {o.icon}
                  <span className="min-w-0 flex-1 truncate">{o.label}</span>
                  {o.hint && <span className="shrink-0 truncate text-xs text-faint">{o.hint}</span>}
                </button>
              );
            })}
            {all.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">{empty}</p>}
          </div>
          {active && (
            <button
              type="button"
              onClick={() => onChange([])}
              className="mt-1 w-full rounded border-t border-line px-2 pt-1.5 pb-1 text-left text-[13px] text-muted hover:text-ink"
            >
              Clear {label.toLowerCase()}
            </button>
          )}
        </MenuKeys>
      )}
    />
  );
}
