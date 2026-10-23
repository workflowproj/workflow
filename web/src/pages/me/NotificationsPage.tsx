import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, CheckCheck, Circle } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Button, EmptyState, IconButton, PageHeader, PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { When } from "@/features/settings/common";
import { cn } from "@/lib/cn";

type Notification = Schemas["Notification"];

export function NotificationsPage() {
  const [unreadOnly, setUnreadOnly] = useState(false);
  const qc = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();
  const q = useQuery({
    queryKey: ["notifications", "list", unreadOnly],
    queryFn: () => unwrap(api.GET("/notifications", { params: { query: { unread: unreadOnly || undefined, limit: 100 } } })),
    refetchInterval: 60_000,
  });
  const mark = useMutation({
    mutationFn: (ids?: number[]) => unwrap(api.POST("/notifications/read", { body: ids ? { ids } : {} })),
    onSuccess: (_, ids) => {
      qc.invalidateQueries({ queryKey: ["notifications"] });
      if (!ids) toast.success("All notifications marked as read");
    },
    onError: toast.error,
  });
  const items = q.data?.items ?? [];
  const unread = q.data?.unread ?? 0;

  const open = (n: Notification) => {
    if (!n.readAt) mark.mutate([n.id]);
    if (n.issueKey) navigate({ to: "/browse/$issueKey", params: { issueKey: n.issueKey } });
  };

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4 p-6">
      <PageHeader
        title="Notifications"
        subtitle={unread ? `${unread} unread` : "You're all caught up"}
        actions={
          <Button icon={<CheckCheck className="size-4" />} disabled={unread === 0} loading={mark.isPending && mark.variables === undefined} onClick={() => mark.mutate(undefined)}>
            Mark all as read
          </Button>
        }
      />
      <div role="tablist" aria-label="Filter notifications" className="flex gap-1 border-b border-line">
        {[
          [false, "All"],
          [true, `Unread${unread ? ` (${unread})` : ""}`],
        ].map(([v, label]) => (
          <button
            key={String(v)}
            role="tab"
            type="button"
            aria-selected={unreadOnly === v}
            onClick={() => setUnreadOnly(v as boolean)}
            className={cn("-mb-px border-b-2 px-3 py-2 text-sm", unreadOnly === v ? "border-accent font-medium text-ink" : "border-transparent text-muted hover:text-ink")}
          >
            {label as string}
          </button>
        ))}
      </div>
      {q.error && <ErrorBox error={q.error} />}
      {q.isLoading ? (
        <PageSpinner />
      ) : items.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState icon={<Bell className="size-8" />} title={unreadOnly ? "No unread notifications" : "No notifications yet"} body="You're notified about issues you report, are assigned to or watch, and when someone mentions you." />
        </div>
      ) : (
        <ul className="divide-y divide-line overflow-hidden rounded-lg border border-line bg-surface">
          {items.map((n) => (
            <li key={n.id} className={cn("group relative flex gap-3 px-4 py-3 hover:bg-surface-2/60", !n.readAt && "bg-accent-soft/30")}>
              <UserAvatar user={n.actor} size={32} />
              <div className="min-w-0 flex-1">
                <button type="button" onClick={() => open(n)} className="block w-full text-left after:absolute after:inset-0 focus-visible:outline-none" aria-label={`${n.title}${n.issueKey ? `, open ${n.issueKey}` : ""}`}>
                  <span className={cn("block text-sm", !n.readAt ? "font-semibold" : "font-medium")}>{n.title}</span>
                </button>
                {n.body && <p className="mt-0.5 line-clamp-2 text-[13px] text-muted">{n.body}</p>}
                <p className="mt-1 flex items-center gap-2 text-xs text-muted">
                  {n.issueKey && (
                    <Link to="/browse/$issueKey" params={{ issueKey: n.issueKey }} className="relative z-10 font-mono text-accent hover:underline" onClick={() => !n.readAt && mark.mutate([n.id])}>
                      {n.issueKey}
                    </Link>
                  )}
                  {n.actor && <span>{n.actor.displayName}</span>}
                  <When iso={n.createdAt} />
                </p>
              </div>
              {!n.readAt ? (
                <IconButton label={`Mark “${n.title}” as read`} size="sm" className="relative z-10 shrink-0" onClick={() => mark.mutate([n.id])}>
                  <Circle className="size-2.5 fill-accent text-accent" />
                </IconButton>
              ) : (
                <span className="size-7 shrink-0" />
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
