import type { ReactNode } from "react";
import { Link, type LinkProps } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { StatusBadge } from "@/components/issue-bits";
import { relativeTime, formatDateTime } from "@/lib/format";
import { RequestTypeIcon } from "@/features/servicedesk/bits";
import type { PortalRequest } from "@/features/servicedesk/api";

/** Crumbs is the portal's breadcrumb trail. */
export function Crumbs({ items }: { items: { label: string; to?: LinkProps["to"]; params?: LinkProps["params"] }[] }) {
  return (
    <nav aria-label="Breadcrumb" className="mb-4 text-[13px] text-muted">
      <ol className="flex flex-wrap items-center gap-1">
        {items.map((c, i) => (
          <li key={i} className="flex items-center gap-1">
            {i > 0 && <ChevronRight aria-hidden className="size-3.5 text-faint" />}
            {c.to ? (
              <Link to={c.to} params={c.params} className="hover:text-ink hover:underline">
                {c.label}
              </Link>
            ) : (
              <span aria-current="page" className="text-ink">
                {c.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

const roleLabel: Record<PortalRequest["role"], string> = { reporter: "", participant: "Shared with you", approver: "Waiting for your approval" };

/** RequestRow is one request in a portal list. */
export function RequestRow({ request: r, extra }: { request: PortalRequest; extra?: ReactNode }) {
  return (
    <li>
      <Link
        to="/portal/requests/$issueKey"
        params={{ issueKey: r.key }}
        className="group flex items-start gap-3 rounded-lg border border-line bg-surface p-3.5 transition-colors hover:border-accent focus-visible:outline-2 focus-visible:outline-accent sm:items-center"
      >
        <RequestTypeIcon icon={r.requestType.icon} size="sm" className="mt-0.5 sm:mt-0" />
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-baseline gap-x-2">
            <span className="font-medium group-hover:text-accent">{r.summary}</span>
            <span className="font-mono text-xs text-muted">{r.key}</span>
          </span>
          <span className="mt-0.5 block text-[13px] text-muted">
            {r.requestType.name} · {r.desk.name}
            {r.role !== "reporter" && <> · raised by {r.reporter.displayName}</>}
            {roleLabel[r.role] && <> · {roleLabel[r.role]}</>} ·{" "}
            <time dateTime={r.updatedAt} title={formatDateTime(r.updatedAt)}>
              updated {relativeTime(r.updatedAt)}
            </time>
          </span>
        </span>
        <span className="flex shrink-0 flex-col items-end gap-1">
          <StatusBadge status={r.status} />
          {extra}
        </span>
      </Link>
    </li>
  );
}
