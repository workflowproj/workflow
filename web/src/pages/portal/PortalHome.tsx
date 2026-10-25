import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Inbox, LifeBuoy } from "lucide-react";
import { useMe } from "@/api/queries";
import { EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { portalDesksQuery, portalRequestsQuery } from "@/features/servicedesk/api";
import { RequestRow } from "./parts";

/** PortalHome is the help centre: the desks the person may ask for help, and their open requests. */
export function PortalHome() {
  const { data: me } = useMe();
  const desks = useQuery(portalDesksQuery);
  const open = useQuery(portalRequestsQuery("open"));
  const recent = (open.data ?? []).slice(0, 3);
  return (
    <div className="flex flex-col gap-8">
      <section aria-labelledby="welcome" className="rounded-xl border border-line bg-surface px-5 py-6 sm:px-8 sm:py-8">
        <h1 id="welcome" className="text-2xl font-semibold tracking-tight">
          {me ? `Hi ${me.displayName.split(" ")[0]}, how can we help?` : "How can we help?"}
        </h1>
        <p className="mt-1.5 max-w-2xl text-sm text-muted">
          Choose a help desk to raise a request. You can follow every request you raise, and reply to the team, from My requests.
        </p>
      </section>

      <section aria-labelledby="desks">
        <h2 id="desks" className="mb-3 text-base font-semibold">
          Help desks
        </h2>
        {desks.error && <ErrorBox error={desks.error} />}
        {desks.isLoading ? (
          <PageSpinner label="Loading help desks" />
        ) : (desks.data ?? []).length === 0 ? (
          <div className="rounded-lg border border-line bg-surface">
            <EmptyState icon={<LifeBuoy className="size-8" />} title="No help desks yet" body="You have not been added to a help desk. Ask the team that invited you." />
          </div>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2">
            {desks.data!.map((d) => (
              <li key={d.projectKey}>
                <Link
                  to="/portal/$projectKey"
                  params={{ projectKey: d.projectKey }}
                  className="group flex h-full items-start gap-3 rounded-lg border border-line bg-surface p-4 transition-colors hover:border-accent focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent">
                    <LifeBuoy className="size-5" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium group-hover:text-accent">{d.name}</span>
                    <span className="mt-0.5 line-clamp-2 block text-[13px] text-muted">{d.description || `${d.requestTypeCount} kinds of request`}</span>
                  </span>
                  <ChevronRight aria-hidden className="mt-2.5 size-4 shrink-0 text-faint" />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>

      {recent.length > 0 && (
        <section aria-labelledby="open-requests">
          <div className="mb-3 flex items-baseline justify-between gap-3">
            <h2 id="open-requests" className="text-base font-semibold">
              Your open requests
            </h2>
            <Link to="/portal/requests" className="text-sm text-accent hover:underline">
              See all
            </Link>
          </div>
          <ul className="flex flex-col gap-2">
            {recent.map((r) => (
              <RequestRow key={r.key} request={r} />
            ))}
          </ul>
        </section>
      )}
      {open.isSuccess && recent.length === 0 && (desks.data ?? []).length > 0 && (
        <p className="flex items-center gap-2 text-sm text-muted">
          <Inbox aria-hidden className="size-4" /> You have no open requests.
        </p>
      )}
    </div>
  );
}
