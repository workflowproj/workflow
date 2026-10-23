import { useState } from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Download, GitBranch, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { issueTypesQuery, useMe } from "@/api/queries";
import { Badge, Button, EmptyState, IconButton, MenuItem, PageHeader, PageSpinner, Popover, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon } from "@/components/issue-bits";
import { CodeBlock, When } from "@/features/settings/common";
import { workflowsQuery, type Workflow } from "@/features/settings/queries";
import { CreateWorkflowDialog } from "@/features/workflow-editor/CreateWorkflowDialog";

const route = getRouteApi("/app/projects/$projectKey/settings/workflows");

export function WorkflowsSettings() {
  const { projectKey } = route.useParams();
  const { data: workflows = [], isLoading, error } = useQuery(workflowsQuery(projectKey));
  const { data: types = [] } = useQuery(issueTypesQuery(projectKey));
  const { data: me } = useMe();
  const [creating, setCreating] = useState<{ copy?: Workflow } | null>(null);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();

  // The project's own workflows, then shared ones (only those this project uses are relevant).
  const own = workflows.filter((w) => !w.shared);
  const shared = workflows.filter((w) => w.shared);
  const typeByKey = new Map(types.map((t) => [t.key, t]));

  const remove = useMutation({
    mutationFn: (w: Workflow) => unwrap(api.DELETE("/workflows/{workflowId}", { params: { path: { workflowId: w.id } } })),
    onSuccess: (_, w) => {
      qc.invalidateQueries({ queryKey: ["workflows"] });
      toast.success(`Deleted “${w.name}”`);
    },
    onError: toast.error,
  });

  const download = async (w: Workflow) => {
    try {
      const d = await qc.fetchQuery({ queryKey: ["workflow", w.id], queryFn: () => unwrap(api.GET("/workflows/{workflowId}", { params: { path: { workflowId: w.id } } })) });
      const url = URL.createObjectURL(new Blob([d.version.yaml], { type: "application/yaml" }));
      Object.assign(document.createElement("a"), { href: url, download: `${w.name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}.yaml` }).click();
      URL.revokeObjectURL(url);
    } catch (e) {
      toast.error(e);
    }
  };

  const row = (w: Workflow) => {
    const here = w.usage.filter((u) => u.projectKey === projectKey);
    const elsewhere = new Set(w.usage.filter((u) => u.projectKey !== projectKey).map((u) => u.projectKey));
    const editable = !w.shared || me?.orgRole === "admin";
    return (
      <tr key={w.id} className="hover:bg-surface-2/60">
        <td className="max-w-md">
          <Link to="/projects/$projectKey/settings/workflows/$workflowId" params={{ projectKey, workflowId: w.id }} className="font-medium hover:text-accent">
            {w.name}
          </Link>
          {w.shared && (
            <Badge className="ml-2" tone="neutral">
              Shared
            </Badge>
          )}
          {w.description && <p className="mt-0.5 truncate text-xs text-muted">{w.description}</p>}
        </td>
        <td>
          <Badge tone="accent">v{w.currentVersion}</Badge>
        </td>
        <td>
          {here.length === 0 && elsewhere.size === 0 ? (
            <span className="text-faint">Unused</span>
          ) : (
            <div className="flex flex-wrap items-center gap-1.5">
              {here.map((u) => {
                const t = typeByKey.get(u.issueTypeKey);
                return (
                  <span key={u.issueTypeKey} className="inline-flex items-center gap-1 rounded bg-surface-2 px-1.5 py-0.5 text-xs">
                    {t && <IssueTypeIcon type={t} className="!size-3.5 [&_svg]:size-2.5" />}
                    {t?.name ?? u.issueTypeKey}
                  </span>
                );
              })}
              {elsewhere.size > 0 && (
                <span className="text-xs text-muted" title={[...elsewhere].join(", ")}>
                  +{elsewhere.size} other project{elsewhere.size === 1 ? "" : "s"}
                </span>
              )}
            </div>
          )}
        </td>
        <td className="text-muted">
          <When iso={w.updatedAt} />
        </td>
        <td className="w-10 text-right">
          <Popover
            align="end"
            trigger={({ toggle }) => (
              <IconButton label={`Actions for ${w.name}`} size="sm" onClick={toggle}>
                <MoreHorizontal className="size-4" />
              </IconButton>
            )}
            content={(close) => (
              <div role="menu">
                <MenuItem
                  icon={<Pencil className="size-4" />}
                  onClick={() => (close(), navigate({ to: "/projects/$projectKey/settings/workflows/$workflowId", params: { projectKey, workflowId: w.id } }))}
                >
                  {editable ? "Edit" : "View"}
                </MenuItem>
                <MenuItem icon={<Copy className="size-4" />} onClick={() => (close(), setCreating({ copy: w }))}>
                  Duplicate
                </MenuItem>
                <MenuItem icon={<Download className="size-4" />} onClick={() => (close(), download(w))}>
                  Download YAML
                </MenuItem>
                {editable && (
                  <MenuItem
                    danger
                    icon={<Trash2 className="size-4" />}
                    disabled={w.usage.length > 0}
                    onClick={async () => {
                      close();
                      if (
                        await confirm({ title: `Delete “${w.name}”?`, body: "The workflow and its version history are deleted. This cannot be undone.", confirmLabel: "Delete workflow", danger: true })
                      )
                        remove.mutate(w);
                    }}
                  >
                    {w.usage.length > 0 ? "Delete (in use)" : "Delete"}
                  </MenuItem>
                )}
              </div>
            )}
          />
        </td>
      </tr>
    );
  };

  return (
    <div className="max-w-5xl">
      <PageHeader
        title="Workflows"
        subtitle="Statuses and transitions issues move through. Each issue type uses one workflow; publishing a new version migrates existing issues in the background."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating({})}>
            New workflow
          </Button>
        }
      />
      {error && <ErrorBox error={error} className="mb-4" />}
      {isLoading ? (
        <PageSpinner />
      ) : workflows.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<GitBranch className="size-8" />}
            title="No workflows yet"
            body="Create a workflow, then pick it for an issue type under Issue types."
            action={<Button onClick={() => setCreating({})}>New workflow</Button>}
          />
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          {[
            ["Project workflows", own],
            ["Shared workflows", shared],
          ].map(([title, list]) =>
            (list as Workflow[]).length === 0 ? null : (
              <section key={title as string} className="rounded-lg border border-line bg-surface">
                <h2 className="border-b border-line px-4 py-3 text-sm font-semibold">{title as string}</h2>
                <div className="overflow-x-auto">
                  <table className={table}>
                    <thead>
                      <tr>
                        <th>Name</th>
                        <th>Version</th>
                        <th>Used by</th>
                        <th>Updated</th>
                        <th>
                          <span className="sr-only">Actions</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>{(list as Workflow[]).map(row)}</tbody>
                  </table>
                </div>
              </section>
            ),
          )}
          <section className="rounded-lg border border-line bg-surface p-4">
            <h2 className="text-sm font-semibold">Workflows as code</h2>
            <p className="mt-1 mb-3 text-sm text-muted">Keep workflow YAML in a repository and apply it from CI with a token that has the write scope. Unchanged documents are a no-op.</p>
            <CodeBlock>{`export WFM_SERVER=${window.location.origin}\nexport WFM_TOKEN=wfm_pat_…\nwfm workflow apply -f workflow.yaml --project ${projectKey} [--map "Old status=New status"] [--dry-run]`}</CodeBlock>
          </section>
        </div>
      )}
      {creating && <CreateWorkflowDialog open onClose={() => setCreating(null)} projectKey={projectKey} workflows={workflows} copyFrom={creating.copy} />}
    </div>
  );
}
