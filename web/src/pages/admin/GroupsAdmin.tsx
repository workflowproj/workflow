import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Search, UsersRound } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, EmptyState, Field, Input, Modal, PageHeader, PageSpinner, Select, Textarea, table, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { SourceBadge } from "@/features/admin/badges";
import { GroupDrawer, ORG_ROLE_MAPPING_HINT } from "@/features/admin/GroupDrawer";
import { useDebounced } from "@/features/settings/common";

type Group = Schemas["Group"];

export function GroupsAdmin() {
  const [q, setQ] = useState("");
  const dq = useDebounced(q, 250);
  const [open, setOpen] = useState<Group | null>(null);
  const [creating, setCreating] = useState(false);
  const groups = useQuery({ queryKey: ["groups", dq], queryFn: () => unwrap(api.GET("/groups", { params: { query: { q: dq || undefined } } })), placeholderData: keepPreviousData });
  const list = groups.data ?? [];
  // Keep the drawer in sync with list refreshes (e.g. after saving).
  const current = open ? (list.find((g) => g.id === open.id) ?? open) : null;

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Groups"
        subtitle="Grant project roles and organisation roles to many people at once. Groups from SCIM or Google Workspace stay in sync with your directory."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
            New group
          </Button>
        }
      />
      <div className="relative w-full max-w-xs">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search groups" aria-label="Search groups" className="pl-8" />
      </div>
      {groups.error && <ErrorBox error={groups.error} />}
      {groups.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<UsersRound className="size-8" />}
            title={dq ? "No groups match" : "No groups yet"}
            body="Create local groups here, or provision them from your identity provider."
            action={!dq && <Button onClick={() => setCreating(true)}>New group</Button>}
          />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>Group</th>
                <th>Source</th>
                <th>Members</th>
                <th>Organisation role</th>
              </tr>
            </thead>
            <tbody>
              {list.map((g) => (
                <tr key={g.id} className="hover:bg-surface-2/60">
                  <td>
                    <button type="button" onClick={() => setOpen(g)} className="text-left">
                      <span className="block font-medium hover:text-accent">{g.displayName}</span>
                      {(g.description || g.email) && <span className="block max-w-md truncate text-xs text-muted">{g.description || g.email}</span>}
                    </button>
                  </td>
                  <td>
                    <SourceBadge source={g.source} />
                  </td>
                  <td>{g.memberCount}</td>
                  <td className="text-muted capitalize">{g.orgRole ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {current && <GroupDrawer key={current.id} group={current} onClose={() => setOpen(null)} />}
      {creating && (
        <CreateGroupDialog
          onClose={() => setCreating(false)}
          onCreated={(g) => {
            setCreating(false);
            setOpen(g);
          }}
        />
      )}
    </div>
  );
}

function CreateGroupDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (g: Group) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [orgRole, setOrgRole] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/admin/groups", { body: { displayName: name.trim(), description, ...(orgRole ? { orgRole: orgRole as Schemas["OrgRole"] } : {}) } })),
    onSuccess: (g) => {
      qc.invalidateQueries({ queryKey: ["groups"] });
      toast.success(`Created ${g.displayName}; add members next.`);
      onCreated(g);
    },
  });
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  const dupe = create.error instanceof ApiError && create.error.code === "duplicate_group" ? create.error.message : undefined;
  return (
    <Modal
      open
      onClose={onClose}
      title="New group"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="group-form" loading={create.isPending} disabled={!name.trim()}>
            Create group
          </Button>
        </>
      }
    >
      <form
        id="group-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field label="Name" required error={fe.displayName ?? dupe}>
          {(id) => <Input id={id} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Payments team" />}
        </Field>
        <Field label="Description">{(id) => <Textarea id={id} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}</Field>
        <Field label="Organisation role for members" hint={ORG_ROLE_MAPPING_HINT}>
          {(id) => (
            <Select id={id} value={orgRole} onChange={(e) => setOrgRole(e.target.value)} className="max-w-56">
              <option value="">No mapping</option>
              <option value="guest">Guest</option>
              <option value="member">Member</option>
              <option value="admin">Admin</option>
            </Select>
          )}
        </Field>
        {create.error && !dupe && Object.keys(fe).length === 0 && <ErrorBox error={create.error} />}
      </form>
    </Modal>
  );
}
