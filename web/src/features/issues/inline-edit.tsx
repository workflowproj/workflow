import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Plus, X } from "lucide-react";
import { labelsQuery, useUsers } from "@/api/queries";
import type { User } from "@/api/types";
import { Popover } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";
import { useEscapeLayer } from "@/lib/escape";

/** Ghost value trigger: looks like text, highlights on hover when editable. */
export const ghost =
  "-mx-2 flex min-h-8 w-[calc(100%+1rem)] min-w-0 items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors " +
  "enabled:hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-default";

const inlineInput = "-mx-2 h-8 w-[calc(100%+1rem)] rounded-md border border-accent bg-surface px-2 text-sm text-ink outline-none ring-2 ring-accent/25";

export function None({ children = "None" }: { children?: ReactNode }) {
  return <span className="text-faint">{children}</span>;
}

/**
 * MenuKeys wraps popover content: focuses the first item and moves focus with the arrow keys.
 * The Popover closes on Escape; focus then returns to the trigger instead of being lost.
 */
export function MenuKeys({ close, children, className }: { close: () => void; children: ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  // Kept in a ref: StrictMode re-runs the effect after focus has already moved into the menu.
  const trigger = useRef<HTMLElement | null>(null);
  useEffect(() => {
    trigger.current ??= document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>("input, [aria-checked=true], [role^=menuitem]:not(:disabled)")?.focus();
    return () => {
      // Closing removed the focused item: hand focus back to the trigger (unless it moved on).
      const t = trigger.current;
      if ((!document.activeElement || document.activeElement === document.body) && t && document.contains(t)) t.focus();
    };
  }, []);
  const items = () => Array.from(ref.current?.querySelectorAll<HTMLElement>("[role^=menuitem]:not(:disabled)") ?? []);
  return (
    <div
      ref={ref}
      role="menu"
      className={className}
      onKeyDown={(e) => {
        if (e.key === "Tab") close();
        else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          e.preventDefault();
          const list = items();
          const i = list.indexOf(document.activeElement as HTMLElement);
          const next = e.key === "ArrowDown" ? (i + 1) % list.length : i <= 0 ? list.length - 1 : i - 1;
          list[next]?.focus();
        }
      }}
    >
      {children}
    </div>
  );
}

function Option({ selected, onClick, children, checkbox }: { selected: boolean; onClick: () => void; children: ReactNode; checkbox?: boolean }) {
  return (
    <button
      type="button"
      role={checkbox ? "menuitemcheckbox" : "menuitemradio"}
      aria-checked={selected}
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2 focus:bg-surface-2 focus:outline-none"
    >
      {checkbox && (
        <span className={cn("flex size-4 shrink-0 items-center justify-center rounded border", selected ? "border-accent bg-accent text-accent-ink" : "border-line-strong")}>
          {selected && <Check className="size-3" strokeWidth={3} />}
        </span>
      )}
      <span className="flex min-w-0 flex-1 items-center gap-2 truncate">{children}</span>
      {!checkbox && selected && <Check className="size-3.5 shrink-0 text-accent" />}
    </button>
  );
}

/** BlockPopover is a Popover whose trigger spans the available width. */
function BlockPopover(props: React.ComponentProps<typeof Popover>) {
  return (
    <div className="min-w-0 [&>div]:w-full">
      <Popover {...props} />
    </div>
  );
}

export type SelectOption = { value: string; label: string; icon?: ReactNode; hint?: string };

/** InlineSelect picks one option from a menu and commits straight away. */
export function InlineSelect({
  value,
  options,
  onCommit,
  disabled,
  allowNone,
  noneLabel = "None",
  label,
  align = "end",
}: {
  value?: string | null;
  options: SelectOption[];
  onCommit: (v: string | null) => void;
  disabled?: boolean;
  allowNone?: boolean;
  noneLabel?: string;
  label: string;
  align?: "start" | "end";
}) {
  const current = options.find((o) => o.value === value);
  return (
    <BlockPopover
      align={align}
      trigger={({ toggle, open }) => (
        <button type="button" className={ghost} disabled={disabled} onClick={toggle} aria-label={label} aria-haspopup="menu" aria-expanded={open}>
          {current ? (
            <>
              {current.icon && (
                <span aria-hidden className="contents">
                  {current.icon}
                </span>
              )}
              <span className="truncate">{current.label}</span>
            </>
          ) : value ? (
            <span className="truncate">{value}</span>
          ) : (
            <None>{noneLabel}</None>
          )}
        </button>
      )}
      content={(close) => (
        <MenuKeys close={close} className="max-h-72 w-60 overflow-y-auto">
          {allowNone && (
            <Option selected={!value} onClick={() => (close(), value && onCommit(null))}>
              <span className="text-muted">{noneLabel}</span>
            </Option>
          )}
          {options.map((o) => (
            <Option key={o.value} selected={o.value === value} onClick={() => (close(), o.value !== value && onCommit(o.value))}>
              {o.icon && (
                <span aria-hidden className="contents">
                  {o.icon}
                </span>
              )}
              <span className="truncate">{o.label}</span>
              {o.hint && <span className="ml-auto text-xs text-faint">{o.hint}</span>}
            </Option>
          ))}
          {options.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">Nothing to choose from</p>}
        </MenuKeys>
      )}
    />
  );
}

/** InlineMulti toggles options on and off; each toggle commits the new list. */
export function InlineMulti({
  values,
  options,
  onCommit,
  disabled,
  label,
  empty,
}: {
  values: string[];
  options: SelectOption[];
  onCommit: (v: string[]) => void;
  disabled?: boolean;
  label: string;
  empty?: ReactNode;
}) {
  const selected = options.filter((o) => values.includes(o.value));
  const unknown = values.filter((v) => !options.some((o) => o.value === v));
  return (
    <BlockPopover
      align="end"
      trigger={({ toggle, open }) => (
        <button type="button" className={cn(ghost, "flex-wrap")} disabled={disabled} onClick={toggle} aria-label={label} aria-haspopup="menu" aria-expanded={open}>
          {selected.length + unknown.length === 0 ? (
            <None />
          ) : (
            <>
              {selected.map((o) => (
                <span key={o.value} className="inline-flex max-w-full items-center gap-1 truncate rounded bg-surface-2 px-1.5 py-0.5 text-[13px]">
                  {o.icon}
                  {o.label}
                </span>
              ))}
              {unknown.map((v) => (
                <span key={v} className="rounded bg-surface-2 px-1.5 py-0.5 text-[13px]">
                  {v}
                </span>
              ))}
            </>
          )}
        </button>
      )}
      content={(close) => (
        <MenuKeys close={close} className="max-h-72 w-60 overflow-y-auto">
          {options.map((o) => (
            <Option
              key={o.value}
              checkbox
              selected={values.includes(o.value)}
              onClick={() => onCommit(values.includes(o.value) ? values.filter((v) => v !== o.value) : [...values, o.value])}
            >
              {o.icon && (
                <span aria-hidden className="contents">
                  {o.icon}
                </span>
              )}
              <span className="truncate">{o.label}</span>
              {o.hint && <span className="ml-auto text-xs text-faint">{o.hint}</span>}
            </Option>
          ))}
          {options.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">{empty ?? "Nothing to choose from"}</p>}
        </MenuKeys>
      )}
    />
  );
}

/** UserMenu is the searchable people list used by user pickers. */
export function UserMenu({
  value,
  onPick,
  project,
  allowNone,
  noneLabel,
  close,
}: {
  value?: User | null;
  onPick: (u: User | null) => void;
  project?: string;
  allowNone?: boolean;
  noneLabel?: string;
  close: () => void;
}) {
  const [q, setQ] = useState("");
  const { data: users = [], isFetching } = useUsers(q, project);
  return (
    <MenuKeys close={close} className="w-64">
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search people…"
        aria-label="Search people"
        onKeyDown={(e) => {
          if (e.key === "Enter" && users[0]) {
            e.preventDefault();
            onPick(users[0]);
            close();
          }
        }}
        className="mb-1 h-8 w-full rounded border border-line bg-surface px-2 text-sm focus:border-accent focus:outline-none"
      />
      <div className="max-h-64 overflow-y-auto">
        {allowNone && (
          <Option selected={!value} onClick={() => (onPick(null), close())}>
            <UserAvatar user={null} size={20} />
            <span className="text-muted">{noneLabel ?? "Unassigned"}</span>
          </Option>
        )}
        {users.map((u) => (
          <Option key={u.id} selected={value?.id === u.id} onClick={() => (onPick(u), close())}>
            <UserAvatar user={u} size={20} />
            <span className="min-w-0 truncate">
              {u.displayName}
              <span className="ml-1.5 text-xs text-faint">{u.email}</span>
            </span>
          </Option>
        ))}
        {!isFetching && users.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">No matches</p>}
      </div>
    </MenuKeys>
  );
}

/** InlineUser shows a person (avatar and name) and changes them through a searchable menu. */
export function InlineUser({
  value,
  onCommit,
  project,
  disabled,
  allowNone = true,
  noneLabel = "Unassigned",
  label,
}: {
  value?: User | null;
  onCommit: (u: User | null) => void;
  project?: string;
  disabled?: boolean;
  allowNone?: boolean;
  noneLabel?: string;
  label: string;
}) {
  return (
    <BlockPopover
      align="end"
      trigger={({ toggle, open }) => (
        <button type="button" className={ghost} disabled={disabled} onClick={toggle} aria-label={label} aria-haspopup="menu" aria-expanded={open}>
          <UserAvatar user={value} size={22} />
          {value ? <span className="truncate">{value.displayName}</span> : <None>{noneLabel}</None>}
        </button>
      )}
      content={(close) => <UserMenu value={value} project={project} allowNone={allowNone} noneLabel={noneLabel} close={close} onPick={(u) => u?.id !== value?.id && onCommit(u)} />}
    />
  );
}

/** InlineText edits a short value in place: Enter or blur saves, Escape cancels. */
export function InlineText({
  value,
  onCommit,
  disabled,
  type = "text",
  label,
  placeholder,
  children,
}: {
  value: string;
  onCommit: (v: string) => void;
  disabled?: boolean;
  type?: "text" | "number" | "url";
  label: string;
  placeholder?: string;
  /** Custom read-only rendering of a non-empty value. */
  children?: ReactNode;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    if (draft === null) return;
    setDraft(null);
    if (draft.trim() !== value) onCommit(draft.trim());
  };
  useEscapeLayer(draft !== null, () => setDraft(null));
  if (draft !== null) {
    return (
      <input
        autoFocus
        aria-label={label}
        type={type === "number" ? "number" : type === "url" ? "url" : "text"}
        step="any"
        value={draft}
        placeholder={placeholder}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          }
        }}
        className={inlineInput}
      />
    );
  }
  return (
    <button type="button" className={ghost} disabled={disabled} onClick={() => setDraft(value)} aria-label={label}>
      {value ? (children ?? <span className="truncate">{value}</span>) : <None />}
    </button>
  );
}

/** InlineDate shows a formatted date; clicking opens the native date picker. */
export function InlineDate({ value, onCommit, disabled, label }: { value?: string | null; onCommit: (v: string | null) => void; disabled?: boolean; label: string }) {
  const [editing, setEditing] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  const last = useRef(value ?? "");
  useEffect(() => {
    last.current = value ?? "";
  }, [value]);
  useEffect(() => {
    if (!editing) return;
    try {
      ref.current?.showPicker?.();
    } catch {
      // Not every browser allows opening the picker programmatically.
    }
  }, [editing]);
  const commit = (v: string) => {
    if (v === last.current) return;
    last.current = v;
    onCommit(v || null);
  };
  useEscapeLayer(editing, () => setEditing(false));
  if (editing) {
    return (
      <input
        ref={ref}
        autoFocus
        type="date"
        aria-label={label}
        defaultValue={value ?? ""}
        onChange={(e) => {
          // Typing a year passes through 0002, 0020... only commit plausible dates.
          const v = e.target.value;
          if (v === "" || Number(v.slice(0, 4)) >= 1900) commit(v);
        }}
        onBlur={(e) => {
          commit(e.target.value);
          setEditing(false);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") (e.target as HTMLInputElement).blur();
        }}
        className={inlineInput}
      />
    );
  }
  return (
    <div className="group flex items-center">
      <button type="button" className={ghost} disabled={disabled} onClick={() => setEditing(true)} aria-label={label}>
        {value ? formatDate(value) : <None />}
      </button>
      {value && !disabled && (
        <button
          type="button"
          aria-label={`Clear ${label.toLowerCase()}`}
          onClick={() => commit("")}
          className="-ml-7 rounded p-1 text-faint opacity-0 group-hover:opacity-100 hover:text-ink focus:opacity-100"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}

/**
 * LabelsEditor is a controlled chip input with suggestions from the project's labels.
 * Enter or comma adds the typed label, Backspace on an empty input removes the last one.
 */
export function LabelsEditor({
  value,
  onChange,
  projectKey,
  autoFocus,
  onDone,
  id,
}: {
  value: string[];
  onChange: (v: string[]) => void;
  projectKey: string;
  autoFocus?: boolean;
  onDone?: () => void;
  id?: string;
}) {
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const listId = useId();
  const { data: known = [] } = useQuery(labelsQuery(projectKey));
  const norm = (s: string) => s.trim().split(/\s+/).join("-");
  const suggestions = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const list = known.filter((l) => !value.includes(l) && l.toLowerCase().includes(needle));
    // Existing labels first (prefix matches before others); creating a new one comes last.
    list.sort((a, b) => Number(!a.toLowerCase().startsWith(needle)) - Number(!b.toLowerCase().startsWith(needle)));
    const shown = list.slice(0, 7);
    if (needle && !known.some((l) => l.toLowerCase() === norm(q).toLowerCase()) && !value.includes(norm(q))) shown.push(norm(q));
    return shown;
  }, [known, q, value]);
  const add = (label: string) => {
    const l = norm(label);
    if (l && !value.includes(l)) onChange([...value, l]);
    setQ("");
    setActive(0);
  };
  return (
    <div className="relative">
      <div className="flex min-h-8.5 flex-wrap items-center gap-1 rounded-md border border-line-strong bg-surface px-1.5 py-1 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/25">
        {value.map((l) => (
          <span key={l} className="inline-flex items-center gap-0.5 rounded bg-surface-2 py-0.5 pr-0.5 pl-1.5 text-[13px]">
            {l}
            <button
              type="button"
              aria-label={`Remove ${l}`}
              onClick={() => onChange(value.filter((x) => x !== l))}
              className="rounded p-0.5 text-faint hover:bg-surface-3 hover:text-ink"
            >
              <X className="size-3" />
            </button>
          </span>
        ))}
        <input
          id={id}
          autoFocus={autoFocus}
          value={q}
          role="combobox"
          aria-expanded={suggestions.length > 0}
          aria-controls={listId}
          aria-label="Add label"
          placeholder={value.length ? "" : "Add labels…"}
          onChange={(e) => {
            setQ(e.target.value.replace(",", ""));
            setActive(0);
            if (e.target.value.includes(",")) add(e.target.value.replace(",", ""));
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              if (q.trim()) add(suggestions[active] ?? q);
              else onDone?.();
            } else if (e.key === "Backspace" && q === "" && value.length) onChange(value.slice(0, -1));
            else if (e.key === "ArrowDown") {
              e.preventDefault();
              setActive((a) => Math.min(a + 1, suggestions.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(a - 1, 0));
            }
          }}
          className="h-6 min-w-20 flex-1 bg-transparent px-1 text-sm outline-none placeholder:text-faint"
        />
      </div>
      {q.trim() !== "" && suggestions.length > 0 && (
        <ul id={listId} role="listbox" className="absolute top-full right-0 left-0 z-30 mt-1 max-h-56 overflow-y-auto rounded-md border border-line bg-surface p-1 shadow-lg">
          {suggestions.map((s, i) => (
            <li key={s} role="option" aria-selected={i === active}>
              <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => add(s)}
                className={cn("flex w-full items-center gap-2 rounded px-2 py-1 text-left text-sm", i === active ? "bg-surface-2" : "hover:bg-surface-2")}
              >
                {!known.includes(s) && <Plus className="size-3.5 text-faint" />}
                {s}
                {!known.includes(s) && <span className="ml-auto text-xs text-faint">new label</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** InlineLabels shows label chips and edits them in place, saving when focus leaves. */
export function InlineLabels({ value, onCommit, projectKey, disabled }: { value: string[]; onCommit: (v: string[]) => void; projectKey: string; disabled?: boolean }) {
  const [draft, setDraft] = useState<string[] | null>(null);
  const done = (v: string[] | null) => {
    setDraft(null);
    if (v && v.join(",") !== value.join(",")) onCommit(v);
  };
  // Escape keeps the labels chosen so far, like clicking away.
  useEscapeLayer(draft !== null, () => done(draft));
  if (draft !== null) {
    return (
      <div
        className="-mx-2"
        onBlur={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node)) done(draft);
        }}
      >
        <LabelsEditor value={draft} onChange={setDraft} projectKey={projectKey} autoFocus onDone={() => done(draft)} />
      </div>
    );
  }
  return (
    <button type="button" className={cn(ghost, "flex-wrap")} disabled={disabled} onClick={() => setDraft(value)} aria-label="Edit labels">
      {value.length === 0 ? (
        <None />
      ) : (
        value.map((l) => (
          <span key={l} className="rounded bg-surface-2 px-1.5 py-0.5 text-[13px]">
            {l}
          </span>
        ))
      )}
    </button>
  );
}
