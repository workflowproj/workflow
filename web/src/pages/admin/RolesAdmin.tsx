import { Fragment, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Lock, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Button, IconButton, MenuItem, PageHeader, PageSpinner, Popover, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { groupPermissions, RoleDialog } from "@/features/admin/RoleDialog";
import { Callout } from "@/features/settings/common";
import { permissionsQuery, rolesQuery } from "@/features/settings/queries";

type Role = Schemas["Role"];

export function RolesAdmin() {
  const roles = useQuery(rolesQuery);
  const perms = useQuery(permissionsQuery);
  const [dialog, setDialog] = useState<{ role?: Role; copyOf?: Role } | null>(null);
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const remove = useMutation({
    mutationFn: (r: Role) => unwrap(api.DELETE("/admin/roles/{roleId}", { params: { path: { roleId: r.id } } })),
    onSuccess: (_, r) => {
      qc.invalidateQueries({ queryKey: ["roles"] });
      toast.success(`Deleted ${r.name}`);
    },
    onError: toast.error,
  });
  const list = roles.data ?? [];
  const sorted = [...list.filter((r) => r.builtin), ...list.filter((r) => !r.builtin)];

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Project roles"
        subtitle="Named sets of permissions granted to people and groups in each project."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setDialog({})}>
            New role
          </Button>
        }
      />
      <Callout>
        Built-in roles can't change; duplicate one to start a custom role. A person's rights in a project are the union of their roles there (direct and through groups); open projects give organisation members the project's open role.
      </Callout>
      {(roles.error || perms.error) && <ErrorBox error={roles.error ?? perms.error} />}
      {roles.isLoading || perms.isLoading ? (
        <PageSpinner />
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="border-b border-line">
                <th className="sticky left-0 min-w-72 bg-surface px-3 py-2 text-left text-xs font-semibold text-muted">Permission</th>
                {sorted.map((r) => (
                  <th key={r.id} className="min-w-32 px-3 py-2 text-center align-bottom">
                    <div className="flex flex-col items-center gap-1">
                      <span className="flex items-center gap-1 text-[13px] font-semibold text-ink">
                        {r.builtin && <Lock className="size-3 text-faint" aria-label="Built-in" />}
                        {r.name}
                      </span>
                      <code className="font-mono text-[11px] font-normal text-faint">{r.key}</code>
                      <Popover
                        align="end"
                        trigger={({ toggle }) => (
                          <IconButton label={`Actions for ${r.name}`} size="sm" className="size-6" onClick={toggle}>
                            <MoreHorizontal className="size-3.5" />
                          </IconButton>
                        )}
                        content={(close) => (
                          <div role="menu" className="text-left font-normal">
                            {!r.builtin && (
                              <MenuItem icon={<Pencil className="size-4" />} onClick={() => (close(), setDialog({ role: r }))}>
                                Edit
                              </MenuItem>
                            )}
                            <MenuItem icon={<Copy className="size-4" />} onClick={() => (close(), setDialog({ copyOf: r }))}>
                              Duplicate
                            </MenuItem>
                            {!r.builtin && (
                              <MenuItem
                                danger
                                icon={<Trash2 className="size-4" />}
                                onClick={async () => {
                                  close();
                                  if (await confirm({ title: `Delete ${r.name}?`, body: "Memberships with this role are removed (people fall back to their other roles), and open projects that use it as their open role become private.", confirmLabel: "Delete role", danger: true })) remove.mutate(r);
                                }}
                              >
                                Delete
                              </MenuItem>
                            )}
                          </div>
                        )}
                      />
                    </div>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {groupPermissions(perms.data ?? []).map((g) => (
                <Fragment key={g.title}>
                  <tr className="bg-surface-2">
                    <td colSpan={sorted.length + 1} className="sticky left-0 px-3 py-1.5 text-[11px] font-semibold tracking-wide text-faint uppercase">
                      {g.title}
                    </td>
                  </tr>
                  {g.perms.map((p) => (
                    <tr key={p.key} className="border-b border-line last:border-b-0 hover:bg-surface-2/50">
                      <td className="sticky left-0 bg-surface px-3 py-2">
                        <span className="block">{p.description}</span>
                        <code className="font-mono text-[11px] text-faint">{p.key}</code>
                      </td>
                      {sorted.map((r) => (
                        <td key={r.id} className="px-3 py-2 text-center">
                          {r.permissions.includes(p.key) ? <Check className="mx-auto size-4 text-success" aria-label={`${r.name} has ${p.key}`} /> : <span className="text-faint" aria-label={`${r.name} lacks ${p.key}`}>·</span>}
                        </td>
                      ))}
                    </tr>
                  ))}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {dialog && <RoleDialog role={dialog.role} copyOf={dialog.copyOf} onClose={() => setDialog(null)} />}
    </div>
  );
}
