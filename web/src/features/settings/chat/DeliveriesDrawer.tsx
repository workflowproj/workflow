import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw, X } from "lucide-react";
import { Badge, Button, Drawer, EmptyState, IconButton, PageSpinner, Spinner, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { When } from "../common";
import { chatDeliveriesQuery, eventLabel, type ChatChannel } from "./meta";

export const deliveryTone = { delivered: "success", failed: "danger", pending: "warning" } as const;

/** DeliveriesDrawer shows a channel's recent messages and their delivery status. */
export function DeliveriesDrawer({ channel, onClose }: { channel: ChatChannel; onClose: () => void }) {
  const q = useQuery(chatDeliveriesQuery(channel.id, 50));
  const list = q.data ?? [];
  return (
    <Drawer open onClose={onClose} label={`Delivery log of ${channel.name}`} width="max-w-2xl">
      <div className="flex items-center justify-between gap-3 border-b border-line px-5 py-3.5">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold">Delivery log · {channel.name}</h2>
          <p className="text-xs text-muted">Most recent first. Network errors, 5xx and 429 answers are retried with backoff.</p>
        </div>
        <div className="flex items-center gap-1">
          <Button size="sm" variant="ghost" icon={q.isFetching ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />} onClick={() => q.refetch()}>
            Refresh
          </Button>
          <IconButton label="Close" size="sm" onClick={onClose}>
            <X className="size-4" />
          </IconButton>
        </div>
      </div>
      {q.error && <ErrorBox error={q.error} className="m-4" />}
      {q.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <EmptyState title="No messages yet" body="Messages appear here when a chosen event happens, or after “Send test”." />
      ) : (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th>Status</th>
                <th>Event</th>
                <th>Response</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {list.map((d) => (
                <tr key={d.id}>
                  <td>
                    <Badge tone={deliveryTone[d.status]}>{d.status}</Badge>
                    {d.attempts > 1 && <span className="ml-1.5 text-xs text-muted">{d.attempts} attempts</span>}
                  </td>
                  <td>
                    <span className="block text-[13px]">{eventLabel(d.event)}</span>
                    {d.issueKey && (
                      <Link to="/browse/$issueKey" params={{ issueKey: d.issueKey }} className="font-mono text-xs text-accent hover:underline">
                        {d.issueKey}
                      </Link>
                    )}
                  </td>
                  <td className="max-w-56 text-xs">
                    {d.responseStatus !== undefined && <span className={d.responseStatus < 300 ? "text-success" : "text-danger"}>HTTP {d.responseStatus}</span>}
                    {d.lastError && (
                      <span className="block break-words text-danger" title={d.lastError}>
                        {d.lastError}
                      </span>
                    )}
                    {d.responseStatus === undefined && !d.lastError && <span className="text-faint">—</span>}
                  </td>
                  <td className="text-muted">
                    <When iso={d.deliveredAt ?? d.createdAt} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Drawer>
  );
}
