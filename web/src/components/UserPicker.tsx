import { useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import { useUsers } from "@/api/queries";
import type { User } from "@/api/types";
import { Popover } from "./ui";
import { UserAvatar } from "./UserAvatar";
import { cn } from "@/lib/cn";

/**
 * UserPicker selects one user (or none). With `project`, only people who can be assigned in
 * that project are offered; with `customers={false}`, only people who can become project
 * members (no help desk customers).
 */
export function UserPicker({
  value,
  onChange,
  project,
  customers = true,
  allowNone = true,
  noneLabel = "Unassigned",
  disabled,
  compact,
  label = "Select user",
}: {
  value?: User | null;
  onChange: (u: User | null) => void;
  project?: string;
  customers?: boolean;
  allowNone?: boolean;
  noneLabel?: string;
  disabled?: boolean;
  compact?: boolean;
  label?: string;
}) {
  const [q, setQ] = useState("");
  const { data: users = [], isFetching } = useUsers(q, project, customers);
  return (
    <Popover
      trigger={({ toggle }) => (
        <button
          type="button"
          disabled={disabled}
          onClick={toggle}
          aria-label={label}
          className={cn(
            "inline-flex max-w-full items-center gap-2 rounded-md text-left text-sm hover:bg-surface-2 disabled:cursor-default disabled:hover:bg-transparent",
            compact ? "p-0.5" : "h-8.5 w-full border border-line-strong bg-surface px-2",
          )}
        >
          <UserAvatar user={value} size={compact ? 24 : 20} />
          {!compact && <span className={cn("flex-1 truncate", !value && "text-muted")}>{value?.displayName ?? noneLabel}</span>}
          {!compact && <ChevronDown className="size-3.5 text-faint" />}
        </button>
      )}
      content={(close) => (
        <div className="w-64">
          <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search people…" className="mb-1 h-8 w-full rounded border border-line bg-surface px-2 text-sm focus:border-accent focus:outline-none" />
          <div className="max-h-64 overflow-y-auto">
            {allowNone && (
              <Row selected={!value} onClick={() => (onChange(null), close())}>
                <UserAvatar user={null} size={20} />
                <span className="text-muted">{noneLabel}</span>
              </Row>
            )}
            {users.map((u) => (
              <Row key={u.id} selected={value?.id === u.id} onClick={() => (onChange(u), close())}>
                <UserAvatar user={u} size={20} />
                <span className="min-w-0 flex-1 truncate">
                  {u.displayName}
                  <span className="ml-1.5 text-xs text-faint">{u.email}</span>
                </span>
              </Row>
            ))}
            {!isFetching && users.length === 0 && <p className="px-2.5 py-2 text-sm text-muted">No matches</p>}
          </div>
        </div>
      )}
    />
  );
}

function Row({ selected, onClick, children }: { selected: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button type="button" onClick={onClick} className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-surface-2">
      {children}
      {selected && <Check className="ml-auto size-3.5 text-accent" />}
    </button>
  );
}
