import { getRouteApi } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Megaphone } from "lucide-react";
import { EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Markdown } from "@/components/Markdown";
import { portalDeskQuery, portalReleaseNotesQuery } from "@/features/servicedesk/api";
import { formatDate } from "@/lib/format";
import { Crumbs } from "./parts";

const route = getRouteApi("/portal/$projectKey/whats-new");

/** PortalWhatsNewPage is a help desk's What's new: the release notes its team published. */
export function PortalWhatsNewPage() {
  const { projectKey } = route.useParams();
  const desk = useQuery(portalDeskQuery(projectKey));
  const notes = useQuery(portalReleaseNotesQuery(projectKey));
  if (desk.isLoading || notes.isLoading) return <PageSpinner label="Loading what's new" />;
  const error = desk.error ?? notes.error;
  if (error || !desk.data) return <ErrorBox error={error ?? new Error("Help desk not found")} />;
  const list = notes.data ?? [];
  return (
    <div>
      <Crumbs items={[{ label: "Help centre", to: "/portal" }, { label: desk.data.name, to: "/portal/$projectKey", params: { projectKey } }, { label: "What's new" }]} />
      <h1 className="text-2xl font-semibold tracking-tight">What's new</h1>
      <p className="mt-1.5 max-w-2xl text-sm text-muted">Release notes from the teams behind {desk.data.name}, newest first.</p>
      {list.length === 0 ? (
        <div className="mt-6 rounded-lg border border-line bg-surface">
          <EmptyState icon={<Megaphone className="size-7" />} title="Nothing new yet" body="Release notes appear here when the team publishes them." />
        </div>
      ) : (
        <ol className="mt-6 flex flex-col gap-4">
          {list.map((n) => (
            <li key={n.id}>
              <article aria-labelledby={`release-${n.id}`} className="rounded-lg border border-line bg-surface p-5">
                <header className="mb-3 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
                  <h2 id={`release-${n.id}`} className="text-lg font-semibold tracking-tight">
                    {n.product} {n.name}
                  </h2>
                  {n.date && (
                    <time dateTime={n.date} className="text-[13px] text-muted">
                      {formatDate(n.date)}
                    </time>
                  )}
                </header>
                <Markdown issueLinks={false}>{n.notes}</Markdown>
              </article>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
