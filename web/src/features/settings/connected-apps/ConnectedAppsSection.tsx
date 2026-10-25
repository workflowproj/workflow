import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import { PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { SettingsSection } from "@/features/settings/common";
import { ConnectedAppsTable } from "./ConnectedApps";

/** ConnectedAppsSection is the profile's list of AI assistants the person connected over OAuth. */
export function ConnectedAppsSection() {
  const qc = useQueryClient();
  const toast = useToast();
  const apps = useQuery({ queryKey: ["my-connected-apps"], queryFn: () => unwrap(api.GET("/me/connected-apps")) });
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/me/connected-apps/{grantId}", { params: { path: { grantId: id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["my-connected-apps"] });
      toast.success("App disconnected");
    },
    onError: toast.error,
  });
  return (
    <SettingsSection
      title="Connected apps"
      description="AI assistants you allowed to act as you after signing in through the browser. Each can read, or read and change, what you can."
    >
      {apps.error && <ErrorBox error={apps.error} className="m-4" />}
      {apps.isLoading ? <PageSpinner /> : <ConnectedAppsTable apps={apps.data ?? []} onRevoke={(a) => revoke.mutate(a.id)} revoking={revoke.isPending ? revoke.variables : undefined} />}
    </SettingsSection>
  );
}
