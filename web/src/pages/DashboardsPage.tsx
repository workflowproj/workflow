import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Globe, LayoutDashboard, Lock, Plus } from "lucide-react";
import { useMe } from "@/api/queries";
import { Badge, Button, PageHeader, PageSpinner, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { dashboardsQuery } from "@/features/dashboards/api";
import { NewDashboardDialog, TemplateCards } from "@/features/dashboards/NewDashboard";
import type { DashboardTemplate } from "@/features/dashboards/templates";
import { formatDateTime, relativeTime } from "@/lib/format";

/** DashboardsPage lists the viewer's dashboards and those shared with everyone (route "/app/dashboards"). */
export function DashboardsPage() {
  const { data: me } = useMe();
  const { data: dashboards, isLoading, error } = useQuery(dashboardsQuery);
  const [creating, setCreating] = useState<DashboardTemplate["id"] | null>(null);
  return (
    <div className="mx-auto max-w-6xl p-4 md:p-6">
      <PageHeader
        title="Dashboards"
        subtitle="Burndowns, velocity, flow and the numbers your team watches, on one page."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating("sprint")}>
            New dashboard
          </Button>
        }
      />
      {error && <ErrorBox error={error} />}
      {isLoading ? (
        <PageSpinner label="Loading dashboards" />
      ) : dashboards?.length === 0 ? (
        <section className="rounded-lg border border-line bg-surface p-6 md:p-8">
          <div className="mx-auto max-w-3xl">
            <div className="mb-6 flex flex-col items-center text-center">
              <LayoutDashboard className="mb-3 size-8 text-faint" />
              <h2 className="text-base font-semibold">Build your first dashboard</h2>
              <p className="mt-1 max-w-xl text-sm text-muted">
                A dashboard gathers charts and numbers from your projects: sprint burndowns and burnups, velocity, cumulative flow, cycle time, breakdowns and issue lists. Keep it to yourself or share it with
                everyone; each person only sees the issues they can browse.
              </p>
            </div>
            <TemplateCards onPick={setCreating} />
          </div>
        </section>
      ) : (
        <div className="overflow-hidden rounded-lg border border-line bg-surface">
          <div className="overflow-x-auto">
            <table className={table}>
              <thead>
                <tr>
                  <th>Name</th>
                  <th className="hidden md:table-cell">Owner</th>
                  <th>Visibility</th>
                  <th className="hidden sm:table-cell">Gadgets</th>
                  <th className="hidden md:table-cell">Updated</th>
                </tr>
              </thead>
              <tbody>
                {dashboards?.map((d) => (
                  <tr key={d.id} className="hover:bg-surface-2">
                    <td className="max-w-md">
                      <Link to="/dashboards/$dashboardId" params={{ dashboardId: d.id }} className="flex items-center gap-2.5 font-medium hover:text-accent">
                        <span className="inline-flex size-6 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent">
                          <LayoutDashboard className="size-3.5" />
                        </span>
                        <span className="truncate">{d.name}</span>
                      </Link>
                      {d.description && <p className="mt-0.5 truncate pl-8.5 text-xs text-muted">{d.description}</p>}
                    </td>
                    <td className="hidden md:table-cell">
                      <span className="inline-flex items-center gap-2">
                        <UserAvatar user={d.owner} size={20} />
                        {d.owner.id === me?.id ? "You" : d.owner.displayName}
                      </span>
                    </td>
                    <td>
                      {d.shared ? (
                        <Badge tone="accent">
                          <Globe className="size-3" /> Shared
                        </Badge>
                      ) : (
                        <Badge>
                          <Lock className="size-3" /> Private
                        </Badge>
                      )}
                    </td>
                    <td className="hidden text-muted tabular-nums sm:table-cell">{d.gadgets.length}</td>
                    <td className="hidden text-muted md:table-cell" title={formatDateTime(d.updatedAt)}>
                      {relativeTime(d.updatedAt)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      <NewDashboardDialog open={creating !== null} template={creating ?? undefined} onClose={() => setCreating(null)} />
    </div>
  );
}
