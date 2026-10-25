import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, KeyRound, Lock, MoreHorizontal, Pencil, Search, UserCheck, UserPlus, UserX } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { useMe } from "@/api/queries";
import { Badge, Button, EmptyState, IconButton, Input, MenuItem, PageHeader, PageSpinner, Popover, Select, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { isDirectoryManaged, SourceBadge, sourceLabel } from "@/features/admin/badges";
import { CreateUserDialog, EditNameDialog, ServiceTokensDialog, type AdminUser } from "@/features/admin/UserDialogs";
import { LockedBadge, SignInSummary, useLocalAccountActions } from "@/features/admin/LocalAccount";
import { useDebounced, When } from "@/features/settings/common";

type Kind = "" | "human" | "service";

export function UsersAdmin() {
  const { data: me } = useMe();
  const [q, setQ] = useState("");
  const dq = useDebounced(q, 250);
  const [kind, setKind] = useState<Kind>("");
  const [status, setStatus] = useState<"" | "active" | "inactive">("");
  const [source, setSource] = useState<"" | Schemas["UserSource"]>("");
  const [dialog, setDialog] = useState<{ kind: "create"; initial: "human" | "service" } | { kind: "rename" | "tokens"; user: AdminUser } | null>(null);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const local = useLocalAccountActions();
  const users = useQuery({
    queryKey: ["admin-users", dq, kind],
    queryFn: () => unwrap(api.GET("/admin/users", { params: { query: { q: dq || undefined, kind: kind || undefined } } })),
    placeholderData: keepPreviousData,
  });
  const update = useMutation({
    mutationFn: ({ user, body }: { user: AdminUser; body: { orgRole?: Schemas["OrgRole"]; active?: boolean } }) =>
      unwrap(api.PATCH("/admin/users/{userId}", { params: { path: { userId: user.id } }, body })),
    onSuccess: (u, { body }) => {
      qc.invalidateQueries({ queryKey: ["admin-users"] });
      toast.success(body.active === undefined ? `${u.displayName} is now ${u.orgRole}` : `${u.displayName} ${u.active ? "reactivated" : "deactivated"}`);
    },
    onError: toast.error,
  });

  const shown = (users.data ?? []).filter((u) => (!status || (status === "active" ? u.active : !u.active)) && (!source || u.source === source));

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Users"
        subtitle="People and service accounts in the organisation."
        actions={
          <>
            <Button icon={<Bot className="size-4" />} onClick={() => setDialog({ kind: "create", initial: "service" })}>
              New service account
            </Button>
            <Button variant="primary" icon={<UserPlus className="size-4" />} onClick={() => setDialog({ kind: "create", initial: "human" })}>
              Add person
            </Button>
          </>
        }
      />
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search name or email" aria-label="Search users" className="pl-8" />
        </div>
        <Select aria-label="Status" value={status} onChange={(e) => setStatus(e.target.value as typeof status)} className="!w-36">
          <option value="">Any status</option>
          <option value="active">Active</option>
          <option value="inactive">Deactivated</option>
        </Select>
        <Select aria-label="Kind" value={kind} onChange={(e) => setKind(e.target.value as Kind)} className="!w-44">
          <option value="">People and bots</option>
          <option value="human">People</option>
          <option value="service">Service accounts</option>
        </Select>
        <Select aria-label="Source" value={source} onChange={(e) => setSource(e.target.value as typeof source)} className="!w-36">
          <option value="">Any source</option>
          {(["google", "keycloak", "scim", "jit", "admin", "dev", "system"] as const).map((s) => (
            <option key={s} value={s}>
              {sourceLabel(s)}
            </option>
          ))}
        </Select>
        <span className="ml-auto text-sm text-muted">{users.data ? `${shown.length} of ${users.data.length}` : ""}</span>
      </div>
      {users.error && <ErrorBox error={users.error} />}
      {users.isLoading ? (
        <PageSpinner />
      ) : shown.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState title="No users match" body="Try another search or filter." />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className={table}>
            <thead>
              <tr>
                <th>User</th>
                <th>Source</th>
                <th className="!w-44">Organisation role</th>
                <th>Status</th>
                <th>Last sign-in</th>
                <th className="w-10">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {shown.map((u) => {
                const self = u.id === me?.id;
                const managed = isDirectoryManaged(u.source);
                return (
                  <tr key={u.id} className={u.active ? undefined : "bg-surface-2/60"}>
                    <td>
                      <span className="flex items-center gap-2.5">
                        <UserAvatar user={u} size={28} />
                        <span className="min-w-0">
                          <span className="flex items-center gap-1.5 font-medium">
                            {u.displayName}
                            {self && <Badge>You</Badge>}
                            {u.kind === "service" && <Badge tone="accent">Service</Badge>}
                            {managed && <Lock className="size-3 text-faint" aria-label={`Profile managed by ${sourceLabel(u.source)}`} />}
                          </span>
                          <span className="block truncate text-xs text-muted">{u.email}</span>
                        </span>
                      </span>
                    </td>
                    <td>
                      <SourceBadge source={u.source} />
                      <SignInSummary user={u} />
                    </td>
                    <td>
                      <Select
                        aria-label={`Organisation role of ${u.displayName}`}
                        value={u.orgRole}
                        disabled={self || update.isPending}
                        title={self ? "You cannot change your own role" : undefined}
                        onChange={async (e) => {
                          const orgRole = e.target.value as Schemas["OrgRole"];
                          if (
                            orgRole === "admin" &&
                            !(await confirm({ title: `Make ${u.displayName} an admin?`, body: "Admins can change every project and the organisation's settings.", confirmLabel: "Make admin" }))
                          )
                            return;
                          update.mutate({ user: u, body: { orgRole } });
                        }}
                        className="!h-8 min-w-28"
                      >
                        {u.kind !== "service" && <option value="admin">Admin</option>}
                        <option value="member">Member</option>
                        <option value="guest">Guest</option>
                      </Select>
                      {u.effectiveOrgRole && u.effectiveOrgRole !== u.orgRole && <p className="mt-0.5 text-[11px] text-muted">Effective: {u.effectiveOrgRole} (via a group)</p>}
                    </td>
                    <td>
                      <span className="flex flex-wrap gap-1">
                        {u.active ? <Badge tone="success">Active</Badge> : <Badge tone="danger">Deactivated</Badge>}
                        <LockedBadge user={u} />
                      </span>
                    </td>
                    <td className="text-muted">
                      <When iso={u.lastLoginAt} empty={u.kind === "service" ? "—" : "Never"} />
                    </td>
                    <td className="text-right">
                      <Popover
                        align="end"
                        portal
                        trigger={({ toggle }) => (
                          <IconButton label={`Actions for ${u.displayName}`} size="sm" onClick={toggle}>
                            <MoreHorizontal className="size-4" />
                          </IconButton>
                        )}
                        content={(close) => (
                          <div role="menu" className="w-60">
                            {u.kind === "service" && (
                              <MenuItem icon={<KeyRound className="size-4" />} onClick={() => (close(), setDialog({ kind: "tokens", user: u }))}>
                                Manage tokens
                              </MenuItem>
                            )}
                            <MenuItem icon={<Pencil className="size-4" />} disabled={managed} onClick={() => (close(), setDialog({ kind: "rename", user: u }))}>
                              {managed ? `Name managed by ${sourceLabel(u.source)}` : "Edit name"}
                            </MenuItem>
                            {u.active ? (
                              <MenuItem
                                danger
                                icon={<UserX className="size-4" />}
                                disabled={self}
                                onClick={async () => {
                                  close();
                                  if (
                                    await confirm({
                                      title: `Deactivate ${u.displayName}?`,
                                      body: (
                                        <>
                                          They are signed out, their tokens stop working and they disappear from pickers. Their issues and history stay.
                                          {managed && ` ${sourceLabel(u.source)} may reactivate them on its next sync; deprovision them in your identity provider instead.`}
                                        </>
                                      ),
                                      confirmLabel: "Deactivate",
                                      danger: true,
                                    })
                                  )
                                    update.mutate({ user: u, body: { active: false } });
                                }}
                              >
                                Deactivate
                              </MenuItem>
                            ) : (
                              <MenuItem icon={<UserCheck className="size-4" />} onClick={() => (close(), update.mutate({ user: u, body: { active: true } }))}>
                                Reactivate
                              </MenuItem>
                            )}
                            {local.menu(u, close)}
                          </div>
                        )}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <p className="flex items-center gap-1.5 text-xs text-muted">
        <Lock className="size-3" /> Profiles provisioned by SCIM or Google directory sync are read-only here; change them in your identity provider.
      </p>
      {dialog?.kind === "create" && <CreateUserDialog initialKind={dialog.initial} onClose={() => setDialog(null)} />}
      {dialog?.kind === "rename" && <EditNameDialog user={dialog.user} onClose={() => setDialog(null)} />}
      {dialog?.kind === "tokens" && <ServiceTokensDialog user={dialog.user} onClose={() => setDialog(null)} />}
      {local.dialogs}
    </div>
  );
}
