import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { PageHeader, PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { SettingsSection, Switch } from "@/features/settings/common";
import { releaseSettingsQuery } from "@/features/releases/queries";

const route = getRouteApi("/app/projects/$projectKey/settings/releases");

/** ReleasesSettings: what a Git tag pushed to a linked repository does to the project's releases. */
export function ReleasesSettings() {
  const { projectKey } = route.useParams();
  const qc = useQueryClient();
  const toast = useToast();
  const { data, isLoading, error } = useQuery(releaseSettingsQuery(projectKey));
  const save = useMutation({
    mutationFn: (body: Schemas["ReleaseSettingsPatch"]) => unwrap(api.PATCH("/projects/{projectKey}/release-settings", { params: { path: { projectKey } }, body })),
    onSuccess: (s) => {
      qc.setQueryData(releaseSettingsQuery(projectKey).queryKey, s);
      toast.success("Release settings saved");
    },
    onError: toast.error,
  });
  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <PageHeader title="Releases" subtitle="What happens when a Git tag reaches a repository linked to this project." />
      {error && <ErrorBox error={error} />}
      {isLoading ? (
        <PageSpinner />
      ) : (
        data && (
          <SettingsSection
            title="Git tags"
            description={
              <>
                A new tag in a{" "}
                <Link to="/projects/$projectKey/settings/repositories" params={{ projectKey }} className="text-accent underline underline-offset-2">
                  linked repository
                </Link>{" "}
                links the release with the same name, with or without a leading v (v3.9.0 and 3.9.0).
              </>
            }
          >
            <div className="flex flex-col divide-y divide-line">
              <div className="flex flex-col gap-1 p-4">
                <Switch checked={data.createFromTags} disabled={save.isPending} onChange={(v) => save.mutate({ createFromTags: v })} label="Create a release for each new tag" />
                <p className="text-[13px] text-muted">A tag that names no release creates one, named after the tag.</p>
              </div>
              <div className="flex flex-col gap-1 p-4">
                <Switch checked={data.releaseOnTag} disabled={save.isPending} onChange={(v) => save.mutate({ releaseOnTag: v })} label="Mark the release released when its tag is pushed" />
                <p className="text-[13px] text-muted">Unfinished issues keep the release as their fix version; its readiness lists them.</p>
              </div>
            </div>
          </SettingsSection>
        )
      )}
    </div>
  );
}
