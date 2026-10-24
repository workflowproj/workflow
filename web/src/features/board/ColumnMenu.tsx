import { useEffect, useRef, useState, type FormEvent } from "react";
import { ArrowLeft, ArrowRight, Gauge, ListChecks, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { Button, IconButton, Input, MenuItem, Popover } from "@/components/ui";
import { cn } from "@/lib/cn";

/** ColumnTools are the column edits offered on a board's column headers (board.manage only). */
export type ColumnTools = {
  rename: (columnId: string, name: string) => void;
  setWipLimit: (columnId: string, limit: number | undefined) => void;
  move: (columnId: string, by: -1 | 1) => void;
  addAfter: (columnId: string, name: string) => void;
  remove: (columnId: string) => void;
  /** Opens the board configuration on its columns. */
  configure: () => void;
};

type Column = { id: string; name: string; wipLimit?: number };

/**
 * ColumnMenu is the "⋯" button on a column header: rename (inline, via `onRename`), WIP limit,
 * move left/right, add a column after, delete, and a way into the full column editor.
 */
export function ColumnMenu({ column, index, count, tools, onRename }: { column: Column; index: number; count: number; tools: ColumnTools; onRename: () => void }) {
  const [view, setView] = useState<"menu" | "wip" | "add">("menu");
  const [open, setOpen] = useState(false);
  const toggle = (v: boolean) => {
    setOpen(v);
    if (!v) setView("menu");
  };
  return (
    <Popover
      align="end"
      open={open}
      onOpenChange={toggle}
      trigger={({ toggle: t, open: o }) => (
        <IconButton
          label={`Column actions for ${column.name}`}
          size="sm"
          aria-haspopup="menu"
          aria-expanded={o}
          onClick={t}
          className={cn("size-6 opacity-0 group-hover/column:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100", o && "opacity-100")}
        >
          <MoreHorizontal className="size-4" />
        </IconButton>
      )}
      content={(close) =>
        view === "wip" ? (
          <WipForm column={column} onCancel={() => setView("menu")} onSave={(n) => (tools.setWipLimit(column.id, n), close())} />
        ) : view === "add" ? (
          <NameForm label={`New column after ${column.name}`} submit="Add column" onCancel={() => setView("menu")} onSave={(name) => (tools.addAfter(column.id, name), close())} />
        ) : (
          <div role="menu" aria-label={`${column.name} column`} className="w-52">
            <MenuItem icon={<Pencil className="size-3.5 text-muted" />} onClick={() => (close(), onRename())}>
              Rename
            </MenuItem>
            <MenuItem icon={<Gauge className="size-3.5 text-muted" />} onClick={() => setView("wip")}>
              {column.wipLimit ? `WIP limit: ${column.wipLimit}` : "Set WIP limit"}
            </MenuItem>
            <MenuItem icon={<ArrowLeft className="size-3.5 text-muted" />} disabled={index === 0} onClick={() => (tools.move(column.id, -1), close())}>
              Move left
            </MenuItem>
            <MenuItem icon={<ArrowRight className="size-3.5 text-muted" />} disabled={index === count - 1} onClick={() => (tools.move(column.id, 1), close())}>
              Move right
            </MenuItem>
            <MenuItem icon={<Plus className="size-3.5 text-muted" />} onClick={() => setView("add")}>
              Add column after
            </MenuItem>
            <MenuItem icon={<ListChecks className="size-3.5 text-muted" />} onClick={() => (close(), tools.configure())}>
              Edit statuses…
            </MenuItem>
            <div className="my-1 border-t border-line" />
            <MenuItem icon={<Trash2 className="size-3.5" />} danger onClick={() => (close(), tools.remove(column.id))}>
              Delete column
            </MenuItem>
          </div>
        )
      }
    />
  );
}

function WipForm({ column, onSave, onCancel }: { column: Column; onSave: (n: number | undefined) => void; onCancel: () => void }) {
  const [value, setValue] = useState(column.wipLimit ? String(column.wipLimit) : "");
  const n = Number(value);
  const valid = value === "" || (Number.isInteger(n) && n >= 1);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) onSave(value === "" ? undefined : n);
  };
  return (
    <form onSubmit={submit} className="flex w-60 flex-col gap-2 p-2">
      <label className="flex flex-col gap-1.5 text-[13px] font-medium">
        WIP limit for {column.name}
        <Input type="number" min={1} step={1} autoFocus value={value} placeholder="No limit" invalid={!valid} onChange={(e) => setValue(e.target.value)} />
      </label>
      <p className="text-xs text-muted">The column turns red when it holds more issues than this.</p>
      <div className="flex items-center gap-1.5">
        <Button type="submit" size="sm" variant="primary" disabled={!valid}>
          Save
        </Button>
        {column.wipLimit != null && (
          <Button size="sm" onClick={() => onSave(undefined)}>
            Remove limit
          </Button>
        )}
        <Button size="sm" variant="ghost" className="ml-auto" onClick={onCancel}>
          Back
        </Button>
      </div>
    </form>
  );
}

function NameForm({ label, submit, onSave, onCancel }: { label: string; submit: string; onSave: (name: string) => void; onCancel: () => void }) {
  const [name, setName] = useState("");
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (name.trim()) onSave(name.trim());
      }}
      className="flex w-60 flex-col gap-2 p-2"
    >
      <label className="flex flex-col gap-1.5 text-[13px] font-medium">
        {label}
        <Input autoFocus maxLength={60} value={name} placeholder="Column name" onChange={(e) => setName(e.target.value)} />
      </label>
      <p className="text-xs text-muted">Map statuses to it from Edit statuses, or from the notice about unmapped statuses.</p>
      <div className="flex items-center gap-1.5">
        <Button type="submit" size="sm" variant="primary" disabled={!name.trim()}>
          {submit}
        </Button>
        <Button size="sm" variant="ghost" className="ml-auto" onClick={onCancel}>
          Back
        </Button>
      </div>
    </form>
  );
}

/** ColumnNameInput renames a column in place: Enter or leaving the field saves, Escape cancels. */
export function ColumnNameInput({ name, onDone }: { name: string; onDone: (name: string | null) => void }) {
  const [value, setValue] = useState(name);
  const ref = useRef<HTMLInputElement>(null);
  const finished = useRef(false);
  useEffect(() => ref.current?.select(), []);
  const finish = (save: boolean) => {
    if (finished.current) return;
    finished.current = true;
    const v = value.trim();
    onDone(save && v && v !== name ? v : null);
  };
  return (
    <input
      ref={ref}
      aria-label="Column name"
      value={value}
      maxLength={60}
      onChange={(e) => setValue(e.target.value)}
      onBlur={() => finish(true)}
      onKeyDown={(e) => {
        if (e.key === "Enter") finish(true);
        else if (e.key === "Escape") {
          e.stopPropagation();
          finish(false);
        }
      }}
      className="h-7 min-w-0 flex-1 rounded border border-accent bg-surface px-1.5 text-[13px] font-medium text-ink focus:ring-2 focus:ring-accent/25 focus:outline-none"
    />
  );
}
