import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Play, RefreshCw } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Badge, Button, EmptyState, PageHeader, PageSpinner, Spinner, table, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ScimSection } from "@/features/admin/ScimSection";
import { directoryName, directorySchedule, directorySettings, directoryTitle, runStats } from "@/features/admin/directory";
import { Callout, SettingsSection, When } from "@/features/settings/common";
import { formatDateTime } from "@/lib/format";

export function ProvisioningAdmin() {
  const q = useQuery({
    queryKey: ["provisioning"],
    queryFn: () => unwrap(api.GET("/admin/provisioning")),
    refetchInterval: (query) => (query.state.data?.directorySync.runs.some((r) => r.status === "running") ? 3000 : false),
  });
  if (q.isLoading) return <PageSpinner />;
  if (q.error || !q.data) return <ErrorBox error={q.error ?? new Error("Not available")} />;
  const { oidc, scim, directorySync } = q.data;
  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="Sign-in & provisioning" subtitle="How people sign in and how accounts and groups are created, updated and removed." />
      <SettingsSection title="Single sign-on" description="Configured with environment variables on the server.">
        <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-[12rem_1fr]">
          <dt className="text-muted">Identity providers</dt>
          <dd className="flex flex-wrap gap-1.5">
            {oidc.providers.length ? (
              oidc.providers.map((p) => (
                <Badge key={p} tone="accent" className="!normal-case">
                  {p}
                </Badge>
              ))
            ) : (
              <span className="text-muted">None — set WFM_OIDC_PROVIDERS (or WFM_GOOGLE_CLIENT_ID / _SECRET).</span>
            )}
          </dd>
          <dt className="text-muted">Allowed email domains</dt>
          <dd className="flex flex-wrap gap-1.5">
            {oidc.allowedDomains.length ? (
              oidc.allowedDomains.map((d) => (
                <code key={d} className="rounded bg-surface-2 px-1.5 font-mono text-[12px]">
                  {d}
                </code>
              ))
            ) : (
              <span className="text-muted">Any domain the provider accepts (WFM_ALLOWED_DOMAINS)</span>
            )}
          </dd>
          <dt className="text-muted">Just-in-time accounts</dt>
          <dd>
            {oidc.jitProvisioning ? <Badge tone="success">On</Badge> : <Badge>Off</Badge>}
            <span className="ml-2 text-[13px] text-muted">
              {oidc.jitProvisioning ? "Anyone who signs in with an allowed domain gets an account." : "Only people provisioned by SCIM, directory sync or an admin can sign in."}
            </span>
          </dd>
          <dt className="text-muted">Bootstrap admins</dt>
          <dd className="text-[13px]">{oidc.bootstrapAdmins.length ? oidc.bootstrapAdmins.join(", ") : <span className="text-muted">None (WFM_BOOTSTRAP_ADMINS)</span>}</dd>
        </dl>
      </SettingsSection>
      <ScimSection scim={scim} />
      <DirectorySync status={directorySync} />
    </div>
  );
}

const runTone = { running: "accent", succeeded: "success", failed: "danger" } as const;

function DirectorySync({ status }: { status: Schemas["DirectorySyncStatus"] }) {
  const qc = useQueryClient();
  const toast = useToast();
  const run = useMutation({
    mutationFn: () => unwrap(api.POST("/admin/directory-sync/run")),
    onSuccess: () => {
      toast.success("Directory sync queued");
      setTimeout(() => qc.invalidateQueries({ queryKey: ["provisioning"] }), 1200);
    },
    onError: toast.error,
  });
  const settings = directorySettings(status);
  return (
    <SettingsSection
      title={directoryTitle(status)}
      description={directorySchedule(status)}
      actions={
        <div className="flex gap-1.5">
          <Button size="sm" variant="ghost" icon={<RefreshCw className="size-3.5" />} onClick={() => qc.invalidateQueries({ queryKey: ["provisioning"] })}>
            Refresh
          </Button>
          <Button size="sm" variant="primary" icon={<Play className="size-3.5" />} disabled={!status.configured} loading={run.isPending} onClick={() => run.mutate()}>
            Run now
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-3 p-4">
        {!status.configured && (
          <Callout title="Sync people and groups from Google Workspace or Keycloak">
            <p>
              <strong className="font-medium text-ink">Google Workspace:</strong> set <code>WFM_GOOGLE_DIRECTORY_ENABLED=true</code>, <code>WFM_GOOGLE_SERVICE_ACCOUNT_KEY</code> (a service account
              with domain-wide delegation for the Admin SDK directory scopes) and <code>WFM_GOOGLE_ADMIN_SUBJECT</code>.
            </p>
            <p className="mt-1.5">
              <strong className="font-medium text-ink">Keycloak:</strong> set <code>WFM_KEYCLOAK_ENABLED=true</code>, <code>WFM_KEYCLOAK_URL</code>, <code>WFM_KEYCLOAK_REALM</code> and{" "}
              <code>WFM_KEYCLOAK_CLIENT_ID</code> / <code>_SECRET</code> for a client whose service account has the realm-management roles <code>view-users</code> and <code>query-groups</code>; people
              sign in through the OIDC provider named by <code>WFM_KEYCLOAK_PROVIDER</code>.
            </p>
            <p className="mt-1.5">Then restart the server. Synced people and groups are read-only here and are deactivated when they are disabled or removed in the directory.</p>
          </Callout>
        )}
        {settings.length > 0 && (
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[12rem_1fr]">
            {settings.map((s) => (
              <div key={s.label} className="contents">
                <dt className="text-muted">{s.label}</dt>
                <dd className="min-w-0 break-words">
                  {s.code ? <code className="rounded bg-surface-2 px-1.5 font-mono text-[12px]">{s.value}</code> : <span className="text-[13px]">{s.value}</span>}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {status.runs.length === 0 ? (
          status.configured && <EmptyState title="No runs yet" body="The first sync runs shortly after start-up, or now with “Run now”." />
        ) : (
          <div className="overflow-x-auto rounded-md border border-line">
            <table className={table}>
              <thead>
                <tr>
                  <th>Started</th>
                  <th>Directory</th>
                  <th>Trigger</th>
                  <th>Status</th>
                  <th>Changes</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                {status.runs.map((r) => (
                  <tr key={r.id}>
                    <td title={formatDateTime(r.startedAt)}>
                      <When iso={r.startedAt} />
                    </td>
                    <td className="whitespace-nowrap">{directoryName(r.source)}</td>
                    <td className="text-muted capitalize">{r.trigger}</td>
                    <td>
                      <Badge tone={runTone[r.status]}>
                        {r.status === "running" && <Spinner className="size-3" />}
                        {r.status}
                      </Badge>
                      {r.error && <p className="mt-1 max-w-xs text-xs text-danger">{r.error}</p>}
                    </td>
                    <td>
                      <span className="flex flex-wrap gap-1">
                        {Object.entries(r.stats).length === 0 ? (
                          <span className="text-faint">—</span>
                        ) : (
                          runStats(r.stats).map((s) => (
                            <span
                              key={s.key}
                              title={s.total ? "In sync after this run" : undefined}
                              className={s.total ? "rounded border border-line px-1.5 py-0.5 text-xs text-muted" : "rounded bg-surface-2 px-1.5 py-0.5 text-xs"}
                            >
                              {s.label}: <strong className="font-semibold">{s.value}</strong>
                            </span>
                          ))
                        )}
                      </span>
                    </td>
                    <td className="text-muted">{r.finishedAt ? `${Math.max(1, Math.round((new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime()) / 1000))} s` : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </SettingsSection>
  );
}
