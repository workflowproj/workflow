import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, Trash2, X } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import type { User } from "@/api/types";
import { Button, Drawer, Field, IconButton, Input, PageSpinner, Select, Textarea, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { UserPicker } from "@/components/UserPicker";
import { Callout } from "@/features/settings/common";
import { SourceBadge, sourceLabel } from "./badges";

type Group = Schemas["Group"];

export const ORG_ROLE_MAPPING_HINT = "Members get at least this organisation role (their own role still applies if higher).";

/** GroupDrawer shows a group's members; local groups can be edited, provisioned ones only map an org role. */
export function GroupDrawer({ group, onClose }: { group: Group; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const local = group.source === "local";
  const detail = useQuery({ queryKey: ["group", group.id], queryFn: () => unwrap(api.GET("/groups/{groupId}", { params: { path: { groupId: group.id } } })) });
  const [name, setName] = useState(group.displayName);
  const [description, setDescription] = useState(group.description);
  const [orgRole, setOrgRole] = useState<string>(group.orgRole ?? "");
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["groups"] });
    qc.invalidateQueries({ queryKey: ["group", group.id] });
    qc.invalidateQueries({ queryKey: ["admin-users"] });
  };
  const save = useMutation({
    mutationFn: () => {
      const body: { displayName?: string; description?: string; orgRole?: Schemas["OrgRole"] | null } = {};
      if (local && name.trim() !== group.displayName) body.displayName = name.trim();
      if (local && description !== group.description) body.description = description;
      if (orgRole !== (group.orgRole ?? "")) body.orgRole = (orgRole || null) as Schemas["OrgRole"] | null;
      return unwrap(api.PATCH("/admin/groups/{groupId}", { params: { path: { groupId: group.id } }, body }));
    },
    onSuccess: () => {
      invalidate();
      toast.success("Group saved");
    },
  });
  const addMember = useMutation({
    mutationFn: (u: User) => unwrap(api.PUT("/admin/groups/{groupId}/members/{userId}", { params: { path: { groupId: group.id, userId: u.id } } })),
    onSuccess: (_, u) => {
      invalidate();
      toast.success(`Added ${u.displayName}`);
    },
    onError: toast.error,
  });
  const removeMember = useMutation({
    mutationFn: (u: User) => unwrap(api.DELETE("/admin/groups/{groupId}/members/{userId}", { params: { path: { groupId: group.id, userId: u.id } } })),
    onSuccess: () => invalidate(),
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/admin/groups/{groupId}", { params: { path: { groupId: group.id } } })),
    onSuccess: () => {
      invalidate();
      toast.success(`Deleted ${group.displayName}`);
      onClose();
    },
    onError: toast.error,
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const dirty = (local && (name.trim() !== group.displayName || description !== group.description)) || orgRole !== (group.orgRole ?? "");
  const members = detail.data?.members ?? [];

  return (
    <Drawer open onClose={onClose} label={group.displayName} width="max-w-xl">
      <div className="flex items-center justify-between gap-3 border-b border-line px-5 py-3.5">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold">{group.displayName}</h2>
          <p className="flex items-center gap-1.5 text-xs text-muted">
            <SourceBadge source={group.source} /> {group.email}
          </p>
        </div>
        <IconButton label="Close" size="sm" onClick={onClose}>
          <X className="size-4" />
        </IconButton>
      </div>
      <div className="flex flex-col gap-5 p-5">
        {!local && (
          <Callout icon={<Lock className="mt-0.5 size-4 shrink-0 text-accent" />} title={`Managed by ${sourceLabel(group.source)}`}>
            Name, description and membership sync from your directory. Only the organisation role mapping can be changed here.
          </Callout>
        )}
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Name" error={fe.displayName}>
            {(id) => <Input id={id} value={name} disabled={!local} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label="Description">{(id) => <Textarea id={id} rows={2} value={description} disabled={!local} onChange={(e) => setDescription(e.target.value)} />}</Field>
          <Field label="Organisation role for members" hint={ORG_ROLE_MAPPING_HINT} error={fe.orgRole}>
            {(id) => (
              <Select id={id} value={orgRole} onChange={(e) => setOrgRole(e.target.value)} className="max-w-56">
                <option value="">No mapping</option>
                <option value="guest">Guest</option>
                <option value="member">Member</option>
                <option value="admin">Admin</option>
              </Select>
            )}
          </Field>
          {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
          <div>
            <Button type="submit" variant="primary" disabled={!dirty} loading={save.isPending}>
              Save
            </Button>
          </div>
        </form>

        <section>
          <div className="mb-2 flex items-center justify-between gap-3">
            <h3 className="text-sm font-semibold">Members ({members.length})</h3>
            {local && (
              <div className="w-56">
                <UserPicker value={null} onChange={(u) => u && addMember.mutate(u)} allowNone={false} noneLabel="Add a member…" label="Add a member" />
              </div>
            )}
          </div>
          {detail.error && <ErrorBox error={detail.error} />}
          {detail.isLoading ? (
            <PageSpinner />
          ) : members.length === 0 ? (
            <p className="rounded-md border border-dashed border-line px-3 py-4 text-center text-sm text-muted">No members yet.</p>
          ) : (
            <ul className="divide-y divide-line rounded-md border border-line">
              {members.map((m) => (
                <li key={m.id} className="flex items-center gap-2.5 px-3 py-2">
                  <UserAvatar user={m} size={24} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">{m.displayName}</span>
                    <span className="block truncate text-xs text-muted">{m.email}</span>
                  </span>
                  {local && (
                    <IconButton label={`Remove ${m.displayName}`} size="sm" onClick={() => removeMember.mutate(m)}>
                      <X className="size-4" />
                    </IconButton>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>

        {local && (
          <section className="border-t border-line pt-4">
            <Button
              icon={<Trash2 className="size-4 text-danger" />}
              loading={remove.isPending}
              onClick={async () => {
                if (
                  await confirm({
                    title: `Delete ${group.displayName}?`,
                    body: "Members lose the project roles and organisation role granted through this group.",
                    confirmLabel: "Delete group",
                    danger: true,
                  })
                )
                  remove.mutate();
              }}
            >
              Delete group
            </Button>
          </section>
        )}
      </div>
    </Drawer>
  );
}
