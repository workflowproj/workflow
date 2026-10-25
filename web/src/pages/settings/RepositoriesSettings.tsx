import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, FolderGit2, GitBranch, Link2, Search, Unlink } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { issueTypesQuery, useMe } from "@/api/queries";
import { Badge, Button, EmptyState, IconButton, Input, PageHeader, PageSpinner, Spinner, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CodeBlock, SettingsSection, useDebounced } from "@/features/settings/common";
import { workflowQuery } from "@/features/settings/queries";
import { slug } from "@/features/workflow-editor/model";

const route = getRouteApi("/app/projects/$projectKey/settings/repositories");

type Repo = Schemas["ScmRepository"];

function ProviderBadge({ provider }: { provider: Repo["provider"] }) {
  return <Badge tone={provider === "github" ? "neutral" : "warning"}>{provider === "github" ? "GitHub" : "GitLab"}</Badge>;
}

export function RepositoriesSettings() {
  const { projectKey } = route.useParams();
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const { data: me } = useMe();
  const linked = useQuery({
    queryKey: ["project-repositories", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/repositories", { params: { path: { projectKey } } })),
  });
  const [q, setQ] = useState("");
  const dq = useDebounced(q, 250);
  const available = useQuery({
    queryKey: ["scm-repositories", dq],
    queryFn: () => unwrap(api.GET("/scm/repositories", { params: { query: { q: dq || undefined } } })),
    placeholderData: keepPreviousData,
  });
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["project-repositories", projectKey] });
    qc.invalidateQueries({ queryKey: ["scm-repositories"] });
  };
  const link = useMutation({
    mutationFn: (r: Repo) => unwrap(api.POST("/projects/{projectKey}/repositories", { params: { path: { projectKey } }, body: { repositoryId: r.id } })),
    onSuccess: (r) => {
      invalidate();
      toast.success(`Linked ${r.fullPath}`);
    },
    onError: toast.error,
  });
  const unlink = useMutation({
    mutationFn: (r: Repo) => unwrap(api.DELETE("/projects/{projectKey}/repositories/{repositoryId}", { params: { path: { projectKey, repositoryId: r.id } } })),
    onSuccess: () => {
      invalidate();
      toast.success("Repository unlinked");
    },
    onError: toast.error,
  });

  // Smart-commit commands come from the transitions of the workflows this project uses.
  const { data: types = [] } = useQuery(issueTypesQuery(projectKey));
  const wfs = useQueries({ queries: [...new Set(types.map((t) => t.workflowId))].map((id) => workflowQuery(id)) });
  const commands = [...new Set(wfs.flatMap((w) => w.data?.version.definition.transitions.map((t) => t.id || slug(t.name)) ?? []))].slice(0, 8);

  const linkedIds = new Set((linked.data ?? []).map((r) => r.id));
  const candidates = (available.data ?? []).filter((r) => !linkedIds.has(r.id));
  const noConnections = !available.isLoading && !dq && (available.data?.length ?? 0) === 0;

  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <PageHeader title="Repositories" subtitle="Link repositories so branches, commits, pull requests and pipelines that mention an issue key show on the issue." />
      {linked.error && <ErrorBox error={linked.error} />}
      <SettingsSection title="Linked repositories" description="Only events from linked repositories update this project's issues.">
        {linked.isLoading ? (
          <PageSpinner />
        ) : (linked.data ?? []).length === 0 ? (
          <EmptyState icon={<FolderGit2 className="size-7" />} title="No repositories linked" body="Search below for repositories your organisation's GitHub App or GitLab connection can see." />
        ) : (
          <table className={table}>
            <tbody>
              {(linked.data ?? []).map((r) => (
                <tr key={r.id}>
                  <td>
                    <a href={r.webUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 font-medium hover:text-accent">
                      {r.fullPath} <ExternalLink className="size-3.5 text-faint" />
                    </a>
                  </td>
                  <td>
                    <ProviderBadge provider={r.provider} />
                  </td>
                  <td className="text-muted">
                    <span className="inline-flex items-center gap-1">
                      <GitBranch className="size-3.5" /> {r.defaultBranch}
                    </span>
                  </td>
                  <td className="text-xs text-muted">{r.projectKeys.length > 1 && `Also linked to ${r.projectKeys.filter((k) => k !== projectKey).join(", ")}`}</td>
                  <td className="w-12 text-right">
                    <IconButton
                      label={`Unlink ${r.fullPath}`}
                      size="sm"
                      onClick={async () => {
                        if (
                          await confirm({
                            title: `Unlink ${r.fullPath}?`,
                            body: "New activity from it no longer updates issues. Existing development links stay on issues.",
                            confirmLabel: "Unlink",
                            danger: true,
                          })
                        )
                          unlink.mutate(r);
                      }}
                    >
                      <Unlink className="size-4" />
                    </IconButton>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </SettingsSection>

      <SettingsSection title="Add a repository">
        <div className="flex flex-col gap-3 p-4">
          {noConnections ? (
            <EmptyState
              icon={<FolderGit2 className="size-7" />}
              title="No repositories discovered yet"
              body={
                me?.orgRole === "admin" ? (
                  <>
                    Connect a GitHub App or a GitLab instance under{" "}
                    <Link to="/admin/scm" className="text-accent hover:underline">
                      Administration → GitHub & GitLab
                    </Link>
                    ; repositories appear here after the first sync.
                  </>
                ) : (
                  "Ask an organisation admin to connect GitHub or GitLab; repositories appear here after the first sync."
                )
              }
            />
          ) : (
            <>
              <div className="relative max-w-md">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
                <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search repositories, e.g. acme/checkout" aria-label="Search repositories" className="pl-8" />
                {available.isFetching && <Spinner className="absolute top-1/2 right-2.5 size-3.5 -translate-y-1/2" />}
              </div>
              {candidates.length === 0 ? (
                <p className="text-sm text-muted">{dq ? `No unlinked repositories match “${dq}”.` : "Every discovered repository is already linked."}</p>
              ) : (
                <ul className="divide-y divide-line rounded-md border border-line">
                  {candidates.slice(0, 20).map((r) => (
                    <li key={r.id} className="flex items-center gap-3 px-3 py-2">
                      <ProviderBadge provider={r.provider} />
                      <span className="min-w-0 flex-1 truncate text-sm font-medium">{r.fullPath}</span>
                      {r.projectKeys.length > 0 && <span className="text-xs text-muted">in {r.projectKeys.join(", ")}</span>}
                      <Button size="sm" icon={<Link2 className="size-3.5" />} loading={link.isPending && link.variables?.id === r.id} onClick={() => link.mutate(r)}>
                        Link
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </div>
      </SettingsSection>

      <SettingsSection title="Working with issue keys">
        <div className="grid gap-5 p-4 md:grid-cols-2">
          <div>
            <h3 className="mb-1 text-[13px] font-semibold">Branches and pull requests</h3>
            <p className="mb-2 text-[13px] text-muted">Mention the key in a branch name, commit message or pull request title and it links automatically. Issues suggest a branch name in this form:</p>
            <CodeBlock>{`git checkout -b ${projectKey}-42-fix-login-timeout`}</CodeBlock>
            <p className="mt-2 text-[13px] text-muted">Keys of previous project keys (aliases) still link.</p>
          </div>
          <div>
            <h3 className="mb-1 text-[13px] font-semibold">Smart commits</h3>
            <p className="mb-2 text-[13px] text-muted">Commands after a key run as the committer (matched by email), through the workflow's conditions and validators:</p>
            <CodeBlock>{`git commit -m "${projectKey}-42 #comment Fixed the retry loop"\ngit commit -m "${projectKey}-42 #${commands.find((c) => /start|progress/.test(c)) ?? commands[0] ?? "start-work"}"`}</CodeBlock>
            {commands.length > 0 && (
              <p className="mt-2 text-[13px] text-muted">
                Transitions in this project:{" "}
                {commands.map((c) => (
                  <code key={c} className="mr-1 rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">
                    #{c}
                  </code>
                ))}
              </p>
            )}
          </div>
        </div>
      </SettingsSection>
    </div>
  );
}
