import { useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2, UserPlus, UsersRound } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { useProject } from "@/api/queries";
import { Badge, Button, EmptyState, IconButton, PageHeader, PageSpinner, Select, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { AddMemberDialog } from "@/features/settings/AddMemberDialog";
import { Callout } from "@/features/settings/common";
import { rolesQuery } from "@/features/settings/queries";

const route = getRouteApi("/app/projects/$projectKey/settings/members");

type Member = Schemas["ProjectMember"];

export function MembersSettings() {
  const { projectKey } = route.useParams();
  const { data: project } = useProject(projectKey);
  const { data: roles = [] } = useQuery(rolesQuery);
  const members = useQuery({
    queryKey: ["project-members", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/members", { params: { path: { projectKey } } })),
  });
  const [adding, setAdding] = useState(false);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["project-members", projectKey] });
    qc.invalidateQueries({ queryKey: ["users"] });
  };

  const update = useMutation({
    mutationFn: ({ m, roleKey }: { m: Member; roleKey: string }) =>
      unwrap(api.PATCH("/projects/{projectKey}/members/{memberId}", { params: { path: { projectKey, memberId: m.id } }, body: { roleKey } })),
    onSuccess: (m) => {
      invalidate();
      toast.success(`${m.user?.displayName ?? m.group?.displayName} is now ${m.role.name}`);
    },
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: (m: Member) => unwrap(api.DELETE("/projects/{projectKey}/members/{memberId}", { params: { path: { projectKey, memberId: m.id } } })),
    onSuccess: () => {
      invalidate();
      toast.success("Member removed");
    },
    onError: toast.error,
  });

  const openRole = roles.find((r) => r.key === project?.openRoleKey);
  const list = members.data ?? [];
  const sorted = [...list].sort((a, b) => Number(!!a.user) - Number(!!b.user) || (a.group?.displayName ?? a.user?.displayName ?? "").localeCompare(b.group?.displayName ?? b.user?.displayName ?? ""));

  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <PageHeader
        title="Members"
        subtitle="People and groups with an explicit role in this project."
        actions={
          <Button variant="primary" icon={<UserPlus className="size-4" />} onClick={() => setAdding(true)}>
            Add members
          </Button>
        }
      />
      <Callout title="How access works">
        <ul className="mt-1 list-disc space-y-0.5 pl-4">
          {project?.access === "open" ? (
            <li>
              This project is <strong className="text-ink">open</strong>: every organisation member (not guests) gets the <strong className="text-ink">{openRole?.name ?? project.openRoleKey}</strong>{" "}
              role without being listed here.
            </li>
          ) : (
            <li>
              This project is <strong className="text-ink">private</strong>: only the members below can see it.
            </li>
          )}
          <li>An explicit membership — direct, or through any of their groups — replaces the open role for that person, so it can also restrict access (e.g. Viewer).</li>
          <li>People with several memberships get the union of those roles' permissions. Organisation admins always have full access.</li>
        </ul>
      </Callout>
      {members.error && <ErrorBox error={members.error} />}
      {members.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<UsersRound className="size-8" />}
            title="No explicit members"
            body={
              project?.access === "open"
                ? "Everyone in the organisation can work here with the open role. Add people or groups to give them a different role."
                : "Add people or groups so they can see this private project."
            }
            action={<Button onClick={() => setAdding(true)}>Add members</Button>}
          />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>Member</th>
                <th>Type</th>
                <th className="w-56">Role</th>
                <th className="w-12">
                  <span className="sr-only">Remove</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((m) => {
                const label = m.user?.displayName ?? m.group?.displayName ?? "Unknown";
                return (
                  <tr key={m.id}>
                    <td>
                      {m.user ? (
                        <span className="flex items-center gap-2.5">
                          <UserAvatar user={m.user} size={28} />
                          <span className="min-w-0">
                            <span className="block truncate font-medium">{m.user.displayName}</span>
                            <span className="block truncate text-xs text-muted">{m.user.email}</span>
                          </span>
                        </span>
                      ) : (
                        <span className="flex items-center gap-2.5">
                          <span className="flex size-7 items-center justify-center rounded-full bg-surface-3 text-muted">
                            <UsersRound className="size-4" />
                          </span>
                          <span className="min-w-0">
                            <span className="block truncate font-medium">{m.group?.displayName}</span>
                            <span className="block truncate text-xs text-muted">
                              {m.group?.memberCount} member{m.group?.memberCount === 1 ? "" : "s"}
                              {m.group?.source && m.group.source !== "local" && ` · synced from ${m.group.source}`}
                            </span>
                          </span>
                        </span>
                      )}
                    </td>
                    <td>{m.user ? <Badge>Person</Badge> : <Badge tone="accent">Group</Badge>}</td>
                    <td>
                      <Select aria-label={`Role of ${label}`} value={m.role.key} disabled={update.isPending} onChange={(e) => update.mutate({ m, roleKey: e.target.value })} className="!h-8">
                        {roles.map((r) => (
                          <option key={r.key} value={r.key}>
                            {r.name}
                          </option>
                        ))}
                      </Select>
                    </td>
                    <td className="text-right">
                      <IconButton
                        label={`Remove ${label}`}
                        size="sm"
                        onClick={async () => {
                          if (
                            await confirm({
                              title: `Remove ${label}?`,
                              body:
                                project?.access === "open"
                                  ? "They fall back to the project's open role (organisation members) or lose access (guests)."
                                  : "They lose access to this private project unless another membership grants it.",
                              confirmLabel: "Remove",
                              danger: true,
                            })
                          )
                            remove.mutate(m);
                        }}
                      >
                        <Trash2 className="size-4" />
                      </IconButton>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <AddMemberDialog open={adding} onClose={() => setAdding(false)} projectKey={projectKey} />
    </div>
  );
}
