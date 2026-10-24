import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { ScrollText, Search, X } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import type { User } from "@/api/types";
import { Badge, Button, EmptyState, Input, PageHeader, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { UserPicker } from "@/components/UserPicker";
import { useDebounced, When } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { formatDateTime } from "@/lib/format";

type AuditEvent = Schemas["AuditEvent"];

const PAGE = 50;
const PRESETS = ["user.", "group.", "role.", "token.", "oauth.", "scim.", "project.", "workflow.", "automation.", "scm.", "field."];

export function AuditAdmin() {
  const [action, setAction] = useState("");
  const [actor, setActor] = useState<User | null>(null);
  const prefix = useDebounced(action.trim(), 300);
  const q = useInfiniteQuery({
    queryKey: ["audit", prefix, actor?.id ?? ""],
    queryFn: ({ pageParam }) => unwrap(api.GET("/admin/audit", { params: { query: { action: prefix || undefined, actorId: actor?.id, before: pageParam, limit: PAGE } } })),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => (last.length === PAGE ? last[last.length - 1].id : undefined),
  });
  const events = q.data?.pages.flat() ?? [];

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Audit log" subtitle="Security-relevant changes: people, roles, tokens, provisioning, projects, workflows and integrations." />
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-64">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
          <Input value={action} onChange={(e) => setAction(e.target.value)} placeholder="Action prefix, e.g. scim." aria-label="Filter by action prefix" className="pl-8 font-mono text-[13px]" />
        </div>
        <div className="w-60">
          <UserPicker value={actor} onChange={setActor} noneLabel="Anyone" label="Filter by actor" />
        </div>
        {(action || actor) && (
          <Button
            variant="ghost"
            size="sm"
            icon={<X className="size-3.5" />}
            onClick={() => {
              setAction("");
              setActor(null);
            }}
          >
            Clear
          </Button>
        )}
      </div>
      <div className="flex flex-wrap gap-1.5" aria-label="Common actions">
        {PRESETS.map((p) => (
          <button
            key={p}
            type="button"
            aria-pressed={action === p}
            onClick={() => setAction(action === p ? "" : p)}
            className={cn(
              "rounded-full border px-2.5 py-0.5 font-mono text-xs",
              action === p ? "border-accent bg-accent-soft text-accent" : "border-line text-muted hover:border-line-strong hover:text-ink",
            )}
          >
            {p}*
          </button>
        ))}
      </div>
      {q.error && <ErrorBox error={q.error} />}
      {q.isLoading ? (
        <PageSpinner />
      ) : events.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState icon={<ScrollText className="size-8" />} title="No events" body={action || actor ? "Nothing matches these filters." : "Audit events appear here as people change things."} />
        </div>
      ) : (
        <ol className="divide-y divide-line overflow-hidden rounded-lg border border-line bg-surface">
          {events.map((e) => (
            <EventRow key={e.id} event={e} onAction={(a) => setAction(a)} onActor={(u) => setActor(u)} />
          ))}
        </ol>
      )}
      {q.hasNextPage && (
        <div className="flex justify-center">
          <Button loading={q.isFetchingNextPage} onClick={() => q.fetchNextPage()}>
            Load older events
          </Button>
        </div>
      )}
      {!q.hasNextPage && events.length > PAGE && <p className="text-center text-xs text-muted">That's everything.</p>}
    </div>
  );
}

function EventRow({ event: e, onAction, onActor }: { event: AuditEvent; onAction: (a: string) => void; onActor: (u: User) => void }) {
  const [open, setOpen] = useState(false);
  const entries = Object.entries(e.data ?? {}).filter(([, v]) => v !== null && v !== undefined && v !== "");
  return (
    <li className="flex gap-3 px-4 py-3">
      {e.actor ? (
        <button type="button" onClick={() => onActor(e.actor!)} title={`Show events by ${e.actor.displayName}`} className="shrink-0">
          <UserAvatar user={e.actor} size={28} />
        </button>
      ) : (
        <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-surface-3 text-[10px] font-semibold text-muted uppercase">{e.actorType.slice(0, 3)}</span>
      )}
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <span className="font-medium">{e.actor?.displayName ?? (e.actorType === "system" ? "System" : e.actorType)}</span>
          <button type="button" onClick={() => onAction(e.action)} title="Show only this action">
            <Badge tone="accent" className="font-mono !normal-case">
              {e.action}
            </Badge>
          </button>
          {e.targetType && (
            <span className="text-[13px] text-muted">
              {e.targetType}
              {e.targetId && <code className="ml-1 font-mono text-[11px] text-faint">{e.targetId.length > 13 ? `${e.targetId.slice(0, 8)}…` : e.targetId}</code>}
            </span>
          )}
        </p>
        {entries.length > 0 && (
          <dl className="mt-1.5 flex flex-wrap gap-1.5">
            {entries.slice(0, open ? undefined : 6).map(([k, v]) => (
              <div key={k} className="inline-flex max-w-full items-baseline gap-1 rounded bg-surface-2 px-1.5 py-0.5 text-xs">
                <dt className="text-muted">{k}</dt>
                <dd className="truncate font-medium text-ink" title={display(v)}>
                  {display(v)}
                </dd>
              </div>
            ))}
            {entries.length > 6 && (
              <button type="button" onClick={() => setOpen(!open)} className="text-xs text-accent hover:underline">
                {open ? "Less" : `+${entries.length - 6} more`}
              </button>
            )}
          </dl>
        )}
      </div>
      <div className="shrink-0 text-right text-xs text-muted">
        <span title={formatDateTime(e.createdAt)}>
          <When iso={e.createdAt} />
        </span>
        {e.ip && <span className="block font-mono text-[11px] text-faint">{e.ip}</span>}
      </div>
    </li>
  );
}

/** display renders an audit value readably: lists joined, objects as compact JSON. */
function display(v: unknown): string {
  if (Array.isArray(v)) return v.map(display).join(", ") || "—";
  if (v && typeof v === "object") return JSON.stringify(v);
  if (typeof v === "boolean") return v ? "yes" : "no";
  return String(v);
}
