import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, ShieldCheck } from "lucide-react";
import { EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { RequestTypeIcon } from "@/features/servicedesk/bits";
import { portalDeskQuery, type PortalRequestType } from "@/features/servicedesk/api";
import { Crumbs } from "./parts";

const route = getRouteApi("/portal/$projectKey");

/** groupTypes keeps the request types' order, grouped under their headings ("" first). */
export function groupTypes(types: PortalRequestType[]): { group: string; types: PortalRequestType[] }[] {
  const out: { group: string; types: PortalRequestType[] }[] = [];
  for (const t of types) {
    let g = out.find((x) => x.group === t.group);
    if (!g) out.push((g = { group: t.group, types: [] }));
    g.types.push(t);
  }
  return out.sort((a, b) => Number(a.group !== "") - Number(b.group !== ""));
}

/** PortalDeskPage lists what people can ask a help desk for. */
export function PortalDeskPage() {
  const { projectKey } = route.useParams();
  const { data: desk, isLoading, error } = useQuery(portalDeskQuery(projectKey));
  if (isLoading) return <PageSpinner label="Loading help desk" />;
  if (error || !desk) return <ErrorBox error={error ?? new Error("Help desk not found")} />;
  const groups = groupTypes(desk.requestTypes);
  return (
    <div>
      <Crumbs items={[{ label: "Help centre", to: "/portal" }, { label: desk.name }]} />
      <h1 className="text-2xl font-semibold tracking-tight">{desk.name}</h1>
      {desk.description && <p className="mt-1.5 max-w-2xl text-sm text-muted">{desk.description}</p>}
      <h2 className="mt-7 mb-1 text-base font-semibold">What can we help you with?</h2>
      {groups.length === 0 && (
        <div className="mt-3 rounded-lg border border-line bg-surface">
          <EmptyState title="Nothing to request yet" body="This help desk has no request types yet." />
        </div>
      )}
      {groups.map((g) => (
        <section key={g.group || "_"} aria-label={g.group || "Requests"} className="mt-4">
          {g.group && <h3 className="mb-2 text-[13px] font-semibold tracking-wide text-muted uppercase">{g.group}</h3>}
          <ul className="grid gap-2.5 sm:grid-cols-2">
            {g.types.map((t) => (
              <li key={t.id}>
                <Link
                  to="/portal/$projectKey/new/$requestTypeId"
                  params={{ projectKey: desk.projectKey, requestTypeId: t.id }}
                  className="group flex h-full items-start gap-3 rounded-lg border border-line bg-surface p-4 transition-colors hover:border-accent focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <RequestTypeIcon icon={t.icon} />
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium group-hover:text-accent">{t.name}</span>
                    {t.description && <span className="mt-0.5 block text-[13px] text-muted">{t.description}</span>}
                    {t.needsApproval && (
                      <span className="mt-1.5 inline-flex items-center gap-1 text-xs text-muted">
                        <ShieldCheck aria-hidden className="size-3.5" /> Needs approval
                      </span>
                    )}
                  </span>
                  <ChevronRight aria-hidden className="mt-2.5 size-4 shrink-0 text-faint" />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
