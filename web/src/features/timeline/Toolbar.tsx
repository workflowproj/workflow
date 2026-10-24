import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Crosshair, FolderKanban } from "lucide-react";
import { projectsQuery } from "@/api/queries";
import { Button, MenuLabel, Popover } from "@/components/ui";
import { ProjectAvatar } from "@/layout/AppShell";
import { cn } from "@/lib/cn";
import { ZOOMS, type Zoom } from "./scale";

export type QuickFilters = { overdue: boolean; conflicts: boolean; mine: boolean };

export function Chip({ pressed, onClick, children, title }: { pressed: boolean; onClick: () => void; children: ReactNode; title?: string }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      title={title}
      onClick={onClick}
      className={cn(
        "inline-flex h-8 items-center gap-1.5 rounded-md border px-3 text-[13px] font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
        pressed ? "border-accent bg-accent-soft text-accent" : "border-line-strong bg-surface text-ink hover:bg-surface-2",
      )}
    >
      {children}
    </button>
  );
}

/** Stats are the header counts; the ones that need attention get a coloured dot. */
export function Stats({ counts }: { counts: { inProgress: number; overdue: number; conflicts: number; unscheduled: number } }) {
  const items = [
    { label: "In progress", value: counts.inProgress, dot: "bg-progress" },
    { label: "Overdue", value: counts.overdue, dot: counts.overdue ? "bg-warning" : "bg-line-strong" },
    { label: "Dependency conflicts", value: counts.conflicts, dot: counts.conflicts ? "bg-danger" : "bg-line-strong" },
    { label: "Unscheduled", value: counts.unscheduled, dot: "bg-line-strong" },
  ];
  return (
    <dl className="flex flex-wrap items-center gap-x-5 gap-y-1">
      {items.map((s) => (
        <div key={s.label} className="flex items-center gap-1.5 text-[13px]">
          <span aria-hidden className={cn("size-2 rounded-full", s.dot)} />
          <dt className="text-muted">{s.label}</dt>
          <dd className="font-semibold text-ink tabular-nums">{s.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function ZoomSwitch({ zoom, onChange, onToday }: { zoom: Zoom; onChange: (z: Zoom) => void; onToday: () => void }) {
  return (
    <div className="flex items-center gap-2">
      <div role="radiogroup" aria-label="Zoom" className="flex rounded-md border border-line-strong bg-surface p-0.5">
        {ZOOMS.map((z) => (
          <button
            key={z.id}
            type="button"
            role="radio"
            aria-checked={zoom === z.id}
            onClick={() => onChange(z.id)}
            className={cn("h-7 rounded px-2.5 text-[13px] font-medium", zoom === z.id ? "bg-accent-soft text-accent" : "text-muted hover:text-ink")}
          >
            {z.label}
          </button>
        ))}
      </div>
      <Button size="sm" icon={<Crosshair className="size-3.5" />} onClick={onToday}>
        Today
      </Button>
    </div>
  );
}

/** ProjectPicker chooses which projects the big-picture timeline shows (none = all). */
export function ProjectPicker({ value, onChange }: { value: string[]; onChange: (keys: string[]) => void }) {
  const { data: projects = [] } = useQuery(projectsQuery);
  const label = value.length === 0 ? "All projects" : value.length === 1 ? (projects.find((p) => p.key === value[0])?.name ?? value[0]) : `${value.length} projects`;
  const toggle = (key: string) => onChange(value.includes(key) ? value.filter((k) => k !== key) : [...value, key]);
  return (
    <Popover
      trigger={({ toggle: open, open: isOpen }) => (
        <button
          type="button"
          onClick={open}
          aria-haspopup="menu"
          aria-expanded={isOpen}
          className="inline-flex h-8 items-center gap-2 rounded-md border border-line-strong bg-surface px-2.5 text-[13px] font-medium hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent"
        >
          <FolderKanban className="size-3.5 text-muted" />
          {label}
          <ChevronDown className="size-3.5 text-faint" />
        </button>
      )}
      content={() => (
        <div role="menu" className="max-h-80 w-64 overflow-y-auto">
          <MenuItemCheck checked={value.length === 0} onClick={() => onChange([])}>
            All projects
          </MenuItemCheck>
          <MenuLabel>Projects</MenuLabel>
          {projects.map((p) => (
            <MenuItemCheck key={p.id} checked={value.includes(p.key)} onClick={() => toggle(p.key)}>
              <ProjectAvatar projectKey={p.key} size={18} />
              <span className="min-w-0 flex-1 truncate">{p.name}</span>
              <span className="font-mono text-[11px] text-faint">{p.key}</span>
            </MenuItemCheck>
          ))}
        </div>
      )}
    />
  );
}

function MenuItemCheck({ checked, onClick, children }: { checked: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" role="menuitemcheckbox" aria-checked={checked} onClick={onClick} className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2">
      <span className={cn("flex size-4 shrink-0 items-center justify-center rounded border", checked ? "border-accent bg-accent text-accent-ink" : "border-line-strong")}>
        {checked && <Check className="size-3" strokeWidth={3} />}
      </span>
      {children}
    </button>
  );
}
