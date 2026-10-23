import { useEffect, useState, type ReactNode } from "react";
import { Search, X } from "lucide-react";
import { Button } from "@/components/ui";
import { cn } from "@/lib/cn";
import { hasFilters, NO_FILTERS, type QuickFilters } from "./jql";

/** QuickFilterBar: free-text search (debounced) plus toggle chips; the page turns them into JQL. */
export function QuickFilterBar({ value, onChange, recent = true, children }: { value: QuickFilters; onChange: (f: QuickFilters) => void; recent?: boolean; children?: ReactNode }) {
  const [text, setText] = useState(value.text);
  useEffect(() => setText(value.text), [value.text]);
  useEffect(() => {
    if (text === value.text) return;
    const t = setTimeout(() => onChange({ ...value, text }), 300);
    return () => clearTimeout(t);
  }, [text, value, onChange]);
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative w-56">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-faint" />
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => e.key === "Escape" && setText("")}
          placeholder="Search issues"
          aria-label="Search issues on this board"
          className="h-8 w-full rounded-md border border-line-strong bg-surface pr-7 pl-8 text-sm placeholder:text-faint focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none"
        />
        {text && (
          <button type="button" aria-label="Clear search" onClick={() => setText("")} className="absolute top-1/2 right-1.5 -translate-y-1/2 rounded p-0.5 text-faint hover:text-ink">
            <X className="size-3.5" />
          </button>
        )}
      </div>
      <Chip pressed={value.mine} onClick={() => onChange({ ...value, mine: !value.mine })}>
        Only my issues
      </Chip>
      {recent && (
        <Chip pressed={value.recent} onClick={() => onChange({ ...value, recent: !value.recent })}>
          Recently updated
        </Chip>
      )}
      {children}
      {hasFilters(value) && (
        <Button variant="ghost" size="sm" onClick={() => onChange(NO_FILTERS)}>
          Clear filters
        </Button>
      )}
    </div>
  );
}

function Chip({ pressed, onClick, children }: { pressed: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "h-8 rounded-md border px-3 text-[13px] font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
        pressed ? "border-accent bg-accent-soft text-accent" : "border-line-strong bg-surface text-ink hover:bg-surface-2",
      )}
    >
      {children}
    </button>
  );
}
