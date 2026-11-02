import type { ReactNode } from "react";
import { Check, ChevronDown, GanttChart, Plus } from "lucide-react";
import { MenuLabel, Popover } from "@/components/ui";
import type { TimelineView } from "./views";

/** ViewSwitcher opens one of your saved timelines or one shared with everyone, or starts a new one. */
export function ViewSwitcher({
  views,
  current,
  onPick,
  onNew,
}: {
  views: TimelineView[];
  current?: TimelineView;
  onPick: (view: TimelineView) => void;
  onNew: () => void;
}) {
  const mine = views.filter((v) => v.mine);
  const shared = views.filter((v) => !v.mine);
  return (
    <Popover
      align="end"
      trigger={({ toggle, open }) => (
        <button
          type="button"
          onClick={toggle}
          aria-haspopup="menu"
          aria-expanded={open}
          className="inline-flex h-8 max-w-64 items-center gap-2 rounded-md border border-line-strong bg-surface px-2.5 text-[13px] font-medium hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent"
        >
          <GanttChart className="size-3.5 shrink-0 text-muted" aria-hidden />
          <span className="truncate">{current ? current.name : "Timelines"}</span>
          <ChevronDown className="size-3.5 shrink-0 text-faint" aria-hidden />
        </button>
      )}
      content={(close) => (
        <div role="menu" aria-label="Timelines" className="max-h-96 w-72 overflow-y-auto">
          <MenuLabel>Your timelines</MenuLabel>
          {mine.length === 0 && <p className="px-2.5 py-1 text-[13px] text-faint">None yet.</p>}
          {mine.map((v) => (
            <ViewItem key={v.id} view={v} current={v.id === current?.id} onClick={() => (close(), onPick(v))} detail={v.shared ? "shared" : undefined} />
          ))}
          {shared.length > 0 && (
            <>
              <MenuLabel>Shared with everyone</MenuLabel>
              {shared.map((v) => (
                <ViewItem key={v.id} view={v} current={v.id === current?.id} onClick={() => (close(), onPick(v))} detail={`by ${v.owner.displayName}`} />
              ))}
            </>
          )}
          <div className="my-1 border-t border-line" />
          <MenuButton onClick={() => (close(), onNew())}>
            <Plus className="size-3.5 text-muted" aria-hidden />
            New timeline…
          </MenuButton>
        </div>
      )}
    />
  );
}

function ViewItem({ view, current, onClick, detail }: { view: TimelineView; current: boolean; onClick: () => void; detail?: string }) {
  const count = view.projects.length === 1 ? "1 project" : `${view.projects.length} projects`;
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={current}
      onClick={onClick}
      className="flex w-full items-start gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2"
    >
      <span className="mt-0.5 flex size-3.5 shrink-0 items-center justify-center">{current && <Check className="size-3.5 text-accent" aria-hidden />}</span>
      <span className="min-w-0 flex-1">
        <span className="block truncate">{view.name}</span>
        <span className="block truncate text-xs text-muted">{[count, detail].filter(Boolean).join(" · ")}</span>
      </span>
    </button>
  );
}

function MenuButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" role="menuitem" onClick={onClick} className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2">
      {children}
    </button>
  );
}
