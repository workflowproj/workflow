import { Check, ChevronDown, Columns3 } from "lucide-react";
import { MenuLabel, Popover } from "@/components/ui";
import type { Board } from "./queries";

/** BoardSwitcher names the current board and, when the project has several, offers the others. */
export function BoardSwitcher({ boards, board, onSelect }: { boards: Board[]; board: Board; onSelect: (id: string) => void }) {
  const label = (
    <>
      <Columns3 className="size-4 text-muted" />
      <span className="truncate font-semibold">{board.name}</span>
      <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[11px] font-medium text-muted capitalize">{board.type}</span>
    </>
  );
  if (boards.length < 2) return <div className="flex min-w-0 items-center gap-2 text-sm">{label}</div>;
  return (
    <Popover
      trigger={({ toggle, open }) => (
        <button type="button" onClick={toggle} aria-expanded={open} aria-haspopup="menu" className="flex h-8 min-w-0 items-center gap-2 rounded-md px-2 text-sm hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent">
          {label}
          <ChevronDown className="size-3.5 text-faint" />
        </button>
      )}
      content={(close) => (
        <div role="menu" className="w-64">
          <MenuLabel>Boards</MenuLabel>
          {boards.map((b) => (
            <button
              key={b.id}
              type="button"
              role="menuitemradio"
              aria-checked={b.id === board.id}
              onClick={() => {
                onSelect(b.id);
                close();
              }}
              className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2"
            >
              <span className="min-w-0 flex-1 truncate">{b.name}</span>
              <span className="text-xs text-faint capitalize">{b.type}</span>
              {b.id === board.id ? <Check className="size-3.5 text-accent" /> : <span className="size-3.5" />}
            </button>
          ))}
        </div>
      )}
    />
  );
}
