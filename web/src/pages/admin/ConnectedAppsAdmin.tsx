import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import { PageHeader, PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { SettingsSection } from "@/features/settings/common";
import { ConnectedAppsTable } from "@/features/settings/connected-apps/ConnectedApps";

/** ConnectedAppsAdmin lists every person's connected AI assistants and lets admins disconnect them. */
export function ConnectedAppsAdmin() {
  const qc = useQueryClient();
  const toast = useToast();
  const apps = useQuery({ queryKey: ["admin-connected-apps"], queryFn: () => unwrap(api.GET("/admin/connected-apps")) });
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/admin/connected-apps/{grantId}", { params: { path: { grantId: id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-connected-apps"] });
      toast.success("App disconnected");
    },
    onError: toast.error,
  });
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Connected apps"
        subtitle="AI assistants people connected to Workflow over OAuth. Each acts as the person who approved it, limited to reading or to reading and changing issues."
      />
      <SettingsSection title="Apps with access" description="Disconnecting revokes the app's tokens at once; it has to ask the person again.">
        {apps.error && <ErrorBox error={apps.error} className="m-4" />}
        {apps.isLoading ? <PageSpinner /> : <ConnectedAppsTable showUser apps={apps.data ?? []} onRevoke={(a) => revoke.mutate(a.id)} revoking={revoke.isPending ? revoke.variables : undefined} />}
      </SettingsSection>
    </div>
  );
}
