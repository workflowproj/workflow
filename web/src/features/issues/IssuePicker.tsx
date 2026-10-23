import { useEffect, useId, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import type { Issue } from "@/api/types";
import { Spinner } from "@/components/ui";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { quoteString } from "@/features/search/jql";
import { cn } from "@/lib/cn";
import { issueSearchQuery } from "./issue-api";

const KEY = /^[A-Za-z][A-Za-z0-9_]{1,9}-\d+$/;

export function useDebounced<T>(value: T, ms = 200): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** pickerJql searches by key or text within `scope`, leaving out `exclude`. */
export function pickerJql(text: string, scope?: string, exclude: string[] = []) {
  const q = text.trim();
  const parts = [scope, ...exclude.map((k) => `key != ${k}`)];
  if (KEY.test(q)) parts.push(`key = ${q.toUpperCase()}`);
  else if (q) parts.push(`text ~ ${quoteString(q)}`);
  return `${parts.filter(Boolean).join(" AND ")} ORDER BY updated DESC`.trim();
}

/**
 * IssuePicker is a combobox that searches issues (GET /search) by key or text.
 * `scope` is extra JQL (e.g. `project = PLAT AND level = 1` for epics).
 */
export function IssuePicker({
  onPick,
  onCancel,
  scope,
  exclude,
  placeholder = "Search by key or summary…",
  autoFocus,
  id,
  inline,
}: {
  /** Render results below the input in the flow (inside a popover) rather than floating. */
  inline?: boolean;
  onPick: (issue: Issue) => void;
  onCancel?: () => void;
  scope?: string;
  exclude?: string[];
  placeholder?: string;
  autoFocus?: boolean;
  id?: string;
}) {
  const [text, setText] = useState("");
  const [open, setOpen] = useState(!!autoFocus);
  const [active, setActive] = useState(0);
  const listId = useId();
  const q = useDebounced(text);
  const { data, isFetching, error } = useQuery({ ...issueSearchQuery(pickerJql(q, scope, exclude)), enabled: open || !!inline, placeholderData: keepPreviousData });
  const issues = data?.issues ?? [];
  const pick = (i: Issue) => {
    onPick(i);
    setText("");
    setOpen(false);
  };
  return (
    <div className="relative min-w-0 flex-1">
      <div className="relative">
      <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint" />
      <input
        id={id}
        autoFocus={autoFocus}
        value={text}
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && issues[active] ? `${listId}-${active}` : undefined}
        placeholder={placeholder}
        onFocus={() => setOpen(true)}
        onBlur={() => !inline && setTimeout(() => setOpen(false), 120)}
        onChange={(e) => {
          setText(e.target.value);
          setActive(0);
          setOpen(true);
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((a) => Math.min(a + 1, issues.length - 1));
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((a) => Math.max(a - 1, 0));
          } else if (e.key === "Enter") {
            e.preventDefault();
            if (issues[active]) pick(issues[active]);
          } else if (e.key === "Escape") {
            e.stopPropagation();
            if (open && text) setText("");
            else onCancel?.();
          }
        }}
        className="h-8.5 w-full rounded-md border border-line-strong bg-surface pr-8 pl-8 text-sm text-ink placeholder:text-faint focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none"
      />
      {isFetching && <Spinner className="absolute top-1/2 right-2.5 size-3.5 -translate-y-1/2" />}
      </div>
      {(open || inline) && (
        <ul
          id={listId}
          role="listbox"
          className={cn("max-h-72 overflow-y-auto", inline ? "mt-1" : "absolute top-full right-0 left-0 z-30 mt-1 rounded-md border border-line bg-surface p-1 shadow-lg")}
        >
          {error ? (
            <li className="px-2.5 py-2 text-sm text-danger">Search failed</li>
          ) : issues.length === 0 ? (
            <li className="px-2.5 py-2 text-sm text-muted">{isFetching ? "Searching…" : "No matching issues"}</li>
          ) : (
            issues.map((i, n) => (
              <li key={i.id} id={`${listId}-${n}`} role="option" aria-selected={n === active}>
                <button
                  type="button"
                  tabIndex={-1}
                  onMouseDown={(e) => e.preventDefault()}
                  onMouseEnter={() => setActive(n)}
                  onClick={() => pick(i)}
                  className={cn("flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm", n === active && "bg-surface-2")}
                >
                  <IssueTypeIcon type={i.type} />
                  <span className="shrink-0 font-mono text-xs text-muted">{i.key}</span>
                  <span className="min-w-0 flex-1 truncate">{i.summary}</span>
                  <StatusBadge status={i.status} className="shrink-0" />
                </button>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}
