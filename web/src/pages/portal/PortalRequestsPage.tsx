import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Inbox, ShieldCheck } from "lucide-react";
import { EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { portalApprovalsQuery, portalRequestsQuery, type RequestFilter } from "@/features/servicedesk/api";
import { RequestRow } from "./parts";

const route = getRouteApi("/portal/requests");

const filters: { value: RequestFilter; label: string }[] = [
  { value: "open", label: "Open" },
  { value: "closed", label: "Closed" },
  { value: "all", label: "All" },
];

/** PortalRequestsPage is "My requests": what the person raised and what is shared with them. */
export function PortalRequestsPage() {
  const { status = "open" } = route.useSearch();
  const navigate = useNavigate();
  const q = useQuery(portalRequestsQuery(status));
  const list = q.data ?? [];
  return (
    <div>
      <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">My requests</h1>
          <p className="mt-1 text-sm text-muted">Requests you raised, and requests others shared with you.</p>
        </div>
        <div role="group" aria-label="Show requests" className="inline-flex rounded-md border border-line bg-surface p-0.5">
          {filters.map((f) => (
            <button
              key={f.value}
              type="button"
              aria-pressed={status === f.value}
              onClick={() => navigate({ to: "/portal/requests", search: { status: f.value } })}
              className={cn("rounded px-3 py-1 text-sm", status === f.value ? "bg-accent-soft font-medium text-accent" : "text-muted hover:text-ink")}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>
      {q.error && <ErrorBox error={q.error} />}
      {q.isLoading ? (
        <PageSpinner label="Loading your requests" />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<Inbox className="size-8" />}
            title={status === "closed" ? "No closed requests" : status === "open" ? "No open requests" : "No requests yet"}
            body="Need something? Raise a request from the help centre."
            action={
              <Link to="/portal" className="text-sm font-medium text-accent hover:underline">
                Get help
              </Link>
            }
          />
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((r) => (
            <RequestRow key={r.key} request={r} />
          ))}
        </ul>
      )}
    </div>
  );
}

/** PortalApprovalsPage lists the requests waiting for the person's approval. */
export function PortalApprovalsPage() {
  const q = useQuery(portalApprovalsQuery);
  const list = q.data ?? [];
  return (
    <div>
      <h1 className="text-2xl font-semibold tracking-tight">Approvals</h1>
      <p className="mt-1 mb-5 text-sm text-muted">Requests that wait for your decision before the team starts on them.</p>
      {q.error && <ErrorBox error={q.error} />}
      {q.isLoading ? (
        <PageSpinner label="Loading approvals" />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState icon={<ShieldCheck className="size-8" />} title="Nothing to approve" body="When someone needs your approval, it shows up here and we email you." />
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((r) => (
            <RequestRow key={r.key} request={r} />
          ))}
        </ul>
      )}
    </div>
  );
}
