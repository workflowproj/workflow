import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Field, Input, Modal, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { permissionsQuery } from "@/features/settings/queries";

type Role = Schemas["Role"];
type PermissionInfo = Schemas["PermissionInfo"];

const AREAS: { title: string; match: (p: string) => boolean }[] = [
  { title: "Project", match: (p) => p.startsWith("project.") },
  { title: "Issues", match: (p) => p.startsWith("issue.") },
  { title: "Comments", match: (p) => p.startsWith("comment.") },
  { title: "Planning and boards", match: (p) => /^(sprint|release|board)\./.test(p) },
];

/** groupPermissions sorts permissions into areas; unknown ones land in "Other". */
export function groupPermissions(perms: PermissionInfo[]): { title: string; perms: PermissionInfo[] }[] {
  const out = AREAS.map((a) => ({ title: a.title, perms: perms.filter((p) => a.match(p.key)) }));
  const other = perms.filter((p) => !AREAS.some((a) => a.match(p.key)));
  if (other.length) out.push({ title: "Other", perms: other });
  return out.filter((g) => g.perms.length);
}

const keyFrom = (name: string) =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^[^a-z]+|-+$/g, "")
    .slice(0, 41);

export function RoleDialog({ role, copyOf, onClose }: { role?: Role; copyOf?: Role; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: perms = [] } = useQuery(permissionsQuery);
  const base = role ?? copyOf;
  const [name, setName] = useState(role?.name ?? (copyOf ? `${copyOf.name} (custom)` : ""));
  const [key, setKey] = useState(role?.key ?? (copyOf ? keyFrom(`${copyOf.key}-custom`) : ""));
  const [keyTouched, setKeyTouched] = useState(!!role);
  const [description, setDescription] = useState(base?.description ?? "");
  const [selected, setSelected] = useState<Set<string>>(new Set([...(base?.permissions ?? []), "project.browse"]));
  const save = useMutation({
    mutationFn: () => {
      const body = { key, name: name.trim(), description, permissions: perms.map((p) => p.key).filter((k) => selected.has(k)) };
      return role ? unwrap(api.PUT("/admin/roles/{roleId}", { params: { path: { roleId: role.id } }, body })) : unwrap(api.POST("/admin/roles", { body }));
    },
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["roles"] });
      toast.success(role ? `Saved ${r.name}` : `Created ${r.name}`);
      onClose();
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const dupe = save.error instanceof ApiError && save.error.code === "duplicate_role" ? save.error.message : undefined;
  const toggle = (k: string, on: boolean) => {
    const next = new Set(selected);
    if (on) next.add(k);
    else next.delete(k);
    setSelected(next);
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={role ? `Edit ${role.name}` : "New project role"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="role-form" loading={save.isPending} disabled={!name.trim() || !/^[a-z][a-z0-9_-]{1,40}$/.test(key)}>
            {role ? "Save role" : "Create role"}
          </Button>
        </>
      }
    >
      <form
        id="role-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="grid gap-4 sm:grid-cols-[1fr_12rem]">
          <Field label="Name" required error={fe.name}>
            {(id) => (
              <Input
                id={id}
                autoFocus
                value={name}
                maxLength={80}
                onChange={(e) => {
                  setName(e.target.value);
                  if (!keyTouched) setKey(keyFrom(e.target.value));
                }}
                placeholder="Triager"
              />
            )}
          </Field>
          <Field label="Key" required error={fe.key ?? dupe} hint="Used in CEL: actor.hasRole(…)">
            {(id) => (
              <Input
                id={id}
                value={key}
                maxLength={41}
                className="font-mono"
                onChange={(e) => {
                  setKeyTouched(true);
                  setKey(e.target.value.toLowerCase().replace(/[^a-z0-9_-]/g, ""));
                }}
                invalid={!!(fe.key ?? dupe) || (key !== "" && !/^[a-z][a-z0-9_-]{1,40}$/.test(key))}
              />
            )}
          </Field>
        </div>
        <Field label="Description">{(id) => <Textarea id={id} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What people with this role do" />}</Field>
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-[13px] font-medium">Permissions</legend>
          {groupPermissions(perms).map((g) => (
            <div key={g.title} className="rounded-md border border-line">
              <p className="border-b border-line bg-surface-2 px-3 py-1.5 text-xs font-semibold text-muted">{g.title}</p>
              <ul className="divide-y divide-line">
                {g.perms.map((p) => {
                  const required = p.key === "project.browse";
                  return (
                    <li key={p.key}>
                      <label className="flex cursor-pointer items-start gap-2.5 px-3 py-2 hover:bg-surface-2/60">
                        <input type="checkbox" className="mt-0.5 size-4 accent-accent" checked={selected.has(p.key)} disabled={required} onChange={(e) => toggle(p.key, e.target.checked)} />
                        <span className="min-w-0">
                          <span className="block text-sm">{p.description}</span>
                          <code className="font-mono text-[11px] text-faint">
                            {p.key}
                            {required && " · required for every role"}
                          </code>
                        </span>
                      </label>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
          {fe.permissions && <p className="text-xs text-danger">{fe.permissions}</p>}
        </fieldset>
        {save.error && !dupe && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}
