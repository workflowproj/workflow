import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { UserRound, UsersRound } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import type { User } from "@/api/types";
import { Button, Field, Modal, Select, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserPicker } from "@/components/UserPicker";
import { cn } from "@/lib/cn";
import { plural } from "@/lib/format";
import { rolesQuery } from "./queries";

export function AddMemberDialog({ open, onClose, projectKey }: { open: boolean; onClose: () => void; projectKey: string }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [kind, setKind] = useState<"user" | "group">("user");
  const [user, setUser] = useState<User | null>(null);
  const [groupId, setGroupId] = useState("");
  const [role, setRole] = useState("member");
  const { data: roles = [] } = useQuery(rolesQuery);
  // The chosen role's description shows under the select, which stays as narrow as its names.
  const chosen = roles.find((r) => r.key === role);
  const { data: groups = [] } = useQuery({ queryKey: ["groups", ""], queryFn: () => unwrap(api.GET("/groups")), enabled: open && kind === "group" });

  const add = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/projects/{projectKey}/members", {
          params: { path: { projectKey } },
          body: kind === "user" ? { userId: user!.id, roleKey: role } : { groupId, roleKey: role },
        }),
      ),
    onSuccess: (m) => {
      qc.invalidateQueries({ queryKey: ["project-members", projectKey] });
      qc.invalidateQueries({ queryKey: ["users"] });
      toast.success(`Added ${m.user?.displayName ?? m.group?.displayName} as ${m.role.name}`);
      setUser(null);
      setGroupId("");
      onClose();
    },
  });
  const fe = add.error instanceof ApiError ? add.error.fieldErrors() : {};
  const ready = kind === "user" ? !!user : !!groupId;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Add members"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={!ready} loading={add.isPending} onClick={() => add.mutate()}>
            Add
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div role="radiogroup" aria-label="Member type" className="grid grid-cols-2 gap-2">
          {(
            [
              ["user", UserRound, "A person"],
              ["group", UsersRound, "A group"],
            ] as const
          ).map(([k, Icon, label]) => (
            <button
              key={k}
              type="button"
              role="radio"
              aria-checked={kind === k}
              onClick={() => setKind(k)}
              className={cn(
                "flex items-center gap-2 rounded-md border px-3 py-2 text-sm",
                kind === k ? "border-accent bg-accent-soft font-medium text-accent" : "border-line hover:border-line-strong",
              )}
            >
              <Icon className="size-4" /> {label}
            </button>
          ))}
        </div>
        {kind === "user" ? (
          <Field label="Person">{() => <UserPicker value={user} onChange={setUser} customers={false} allowNone={false} noneLabel="Choose a person…" label="Person" />}</Field>
        ) : (
          <Field label="Group" hint="Everyone in the group gets the role, now and as membership changes (including groups synced from your directory).">
            {(id) => (
              <Select id={id} value={groupId} onChange={(e) => setGroupId(e.target.value)}>
                <option value="">Choose a group…</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.displayName} ({plural(g.memberCount, "member")}
                    {g.source !== "local" ? `, ${g.source}` : ""})
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}
        <Field label="Role" error={fe.roleKey} hint={chosen && (chosen.description || plural(chosen.permissions.length, "permission"))}>
          {(id) => (
            <Select id={id} value={role} onChange={(e) => setRole(e.target.value)}>
              {roles.map((r) => (
                <option key={r.key} value={r.key}>
                  {r.name}
                </option>
              ))}
            </Select>
          )}
        </Field>
        {add.error && !fe.roleKey && <ErrorBox error={add.error} />}
      </div>
    </Modal>
  );
}
