import { Check } from "lucide-react";
import { Popover } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import type { Person } from "./people";

/**
 * PeopleFilter is a row of assignee avatars; each toggles showing only that person's issues
 * (several can be picked). People beyond `max` are in a "+N" menu.
 */
export function PeopleFilter({ people, selected, onChange, meId, max = 7 }: { people: Person[]; selected: string[]; onChange: (ids: string[]) => void; meId?: string; max?: number }) {
  const shown = people.length > max ? people.slice(0, max - 1) : people;
  const rest = people.slice(shown.length);
  const toggle = (id: string) => onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
  const name = (p: Person) => (p.user ? `${p.user.displayName}${p.id === meId ? " (you)" : ""}` : "Unassigned");
  const label = (p: Person) => `${name(p)}: ${p.count} issue${p.count === 1 ? "" : "s"}`;
  const restSelected = rest.filter((p) => selected.includes(p.id)).length;

  return (
    <div role="group" aria-label="Filter by assignee" className="flex items-center pl-0.5">
      {shown.map((p) => {
        const on = selected.includes(p.id);
        return (
          <button
            key={p.id}
            type="button"
            aria-pressed={on}
            aria-label={label(p)}
            title={label(p)}
            onClick={() => toggle(p.id)}
            className={cn(
              "relative -ml-1 rounded-full bg-surface ring-2 transition-transform first:ml-0 hover:z-20 hover:-translate-y-0.5 focus-visible:z-20 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
              on ? "z-10 ring-accent" : "ring-surface",
            )}
          >
            <UserAvatar user={p.user} size={28} className="pointer-events-none" />
          </button>
        );
      })}
      {rest.length > 0 && (
        <Popover
          align="end"
          className="w-64"
          trigger={({ open, toggle: flip }) => (
            <button
              type="button"
              aria-haspopup="menu"
              aria-expanded={open}
              aria-label={`${rest.length} more ${rest.length === 1 ? "person" : "people"}${restSelected ? `, ${restSelected} selected` : ""}`}
              onClick={flip}
              className={cn(
                "relative -ml-1 inline-flex size-7 items-center justify-center rounded-full bg-surface-3 text-[11px] font-semibold text-muted ring-2 tabular-nums transition-transform hover:z-20 hover:-translate-y-0.5 focus-visible:z-20 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
                restSelected ? "z-10 text-accent ring-accent" : "ring-surface",
              )}
            >
              +{rest.length}
            </button>
          )}
          content={() => (
            <div role="menu" aria-label="More people" className="max-h-72 overflow-y-auto">
              {rest.map((p) => {
                const on = selected.includes(p.id);
                return (
                  <button
                    key={p.id}
                    type="button"
                    role="menuitemcheckbox"
                    aria-checked={on}
                    onClick={() => toggle(p.id)}
                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-surface-2"
                  >
                    <UserAvatar user={p.user} size={20} />
                    <span className="min-w-0 flex-1 truncate">{name(p)}</span>
                    <span className="text-xs text-faint tabular-nums">{p.count}</span>
                    <Check className={cn("size-4 shrink-0 text-accent", !on && "invisible")} />
                  </button>
                );
              })}
            </div>
          )}
        />
      )}
    </div>
  );
}
