import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Layers, Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { issueTypesQuery } from "@/api/queries";
import type { IssueType } from "@/api/types";
import { Button, EmptyState, IconButton, PageHeader, PageSpinner, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon } from "@/components/issue-bits";
import { IssueTypeDialog, LEVELS } from "@/features/settings/IssueTypeDialog";

const route = getRouteApi("/app/projects/$projectKey/settings/issue-types");

export function IssueTypesSettings() {
  const { projectKey } = route.useParams();
  const { data: types = [], isLoading, error } = useQuery(issueTypesQuery(projectKey));
  const [editing, setEditing] = useState<IssueType | "new" | null>(null);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const remove = useMutation({
    mutationFn: (t: IssueType) => unwrap(api.DELETE("/projects/{projectKey}/issue-types/{typeId}", { params: { path: { projectKey, typeId: t.id } } })),
    onSuccess: (_, t) => {
      qc.invalidateQueries({ queryKey: ["issue-types", projectKey] });
      qc.invalidateQueries({ queryKey: ["workflows"] });
      toast.success(`Deleted ${t.name}`);
    },
    onError: toast.error,
  });
  const sorted = [...types].sort((a, b) => b.level - a.level || a.position - b.position);

  return (
    <div className="flex max-w-5xl flex-col gap-5">
      <PageHeader
        title="Issue types"
        subtitle="The kinds of work in this project, their place in the hierarchy and the workflow each one follows."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing("new")}>
            New issue type
          </Button>
        }
      />
      {error && <ErrorBox error={error} />}
      {isLoading ? (
        <PageSpinner />
      ) : types.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState icon={<Layers className="size-8" />} title="No issue types" action={<Button onClick={() => setEditing("new")}>New issue type</Button>} />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>Issue type</th>
                <th>Key</th>
                <th>Level</th>
                <th>Workflow</th>
                <th className="w-20">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((t) => (
                <tr key={t.id} className="hover:bg-surface-2/60">
                  <td>
                    <span className="flex items-center gap-2.5">
                      <IssueTypeIcon type={t} className="size-5 [&_svg]:size-3.5" />
                      <span className="min-w-0">
                        <span className="block font-medium">{t.name}</span>
                        {t.description && <span className="block max-w-sm truncate text-xs text-muted">{t.description}</span>}
                      </span>
                    </span>
                  </td>
                  <td className="font-mono text-xs text-muted">{t.key}</td>
                  <td className="text-muted">{LEVELS.find((l) => l.value === t.level)?.label ?? t.level}</td>
                  <td>
                    <Link to="/projects/$projectKey/settings/workflows/$workflowId" params={{ projectKey, workflowId: t.workflowId }} className="hover:text-accent hover:underline">
                      {t.workflowName}
                    </Link>
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <IconButton label={`Edit ${t.name}`} size="sm" onClick={() => setEditing(t)}>
                      <Pencil className="size-4" />
                    </IconButton>
                    <IconButton
                      label={`Delete ${t.name}`}
                      size="sm"
                      onClick={async () => {
                        if (await confirm({ title: `Delete ${t.name}?`, body: "Only types without issues can be deleted; change existing issues' type first.", confirmLabel: "Delete", danger: true })) remove.mutate(t);
                      }}
                    >
                      <Trash2 className="size-4" />
                    </IconButton>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing && <IssueTypeDialog open onClose={() => setEditing(null)} projectKey={projectKey} type={editing === "new" ? undefined : editing} />}
    </div>
  );
}
