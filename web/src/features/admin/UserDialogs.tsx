import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Plus, UserRound } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { providersQuery } from "@/api/queries";
import { Button, Field, Input, Modal, PageSpinner, Select, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CodeBlock } from "@/features/settings/common";
import { CreateTokenDialog, TokenTable } from "@/features/settings/tokens/Tokens";
import { GroupChoice, SignInChoice, startSignIn, TemporaryPasswordDialog, type SignInMethod } from "@/features/admin/LocalAccount";
import { cn } from "@/lib/cn";

export type AdminUser = Schemas["AdminUser"];

export function CreateUserDialog({ onClose, initialKind = "human" }: { onClose: () => void; initialKind?: "human" | "service" }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [kind, setKind] = useState<"human" | "service">(initialKind);
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [orgRole, setOrgRole] = useState<Schemas["OrgRole"]>("member");
  // People sign in with single sign-on, or (local accounts) a password from an invitation or a temporary one.
  const { data: providers } = useQuery(providersQuery);
  const sso = (providers?.providers.length ?? 0) > 0;
  const local = !!providers?.localLogin;
  const [chosen, setMethod] = useState<SignInMethod | null>(null);
  const method: SignInMethod = !local ? "sso" : (chosen ?? (sso ? "sso" : "invite"));
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [temp, setTemp] = useState<{ user: AdminUser; password: string } | null>(null);
  const create = useMutation({
    mutationFn: async () => {
      const human = kind === "human";
      const u = await unwrap(
        api.POST("/admin/users", { body: { kind, displayName: displayName.trim(), orgRole, ...(email.trim() ? { email: email.trim() } : {}), ...(human && groupIds.length ? { groupIds } : {}) } }),
      );
      try {
        return { u, password: human ? await startSignIn(u.id, method) : null, failed: null };
      } catch (e) {
        return { u, password: null, failed: e };
      }
    },
    onSuccess: ({ u, password, failed }) => {
      qc.invalidateQueries({ queryKey: ["admin-users"] });
      if (failed) {
        toast.error(new Error(`Added ${u.displayName}, but ${method === "invite" ? "the invitation" : "setting a temporary password"} failed: ${failed instanceof Error ? failed.message : failed}`));
      } else if (password) {
        setTemp({ user: u, password });
        return;
      } else {
        toast.success(
          kind === "service" ? `Created service account ${u.displayName}. Issue it a token from the user list.` : method === "invite" ? `Added ${u.displayName} and emailed an invitation` : `Added ${u.displayName}`,
        );
      }
      onClose();
    },
  });
  if (temp) return <TemporaryPasswordDialog user={temp.user} password={temp.password} onClose={onClose} />;
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  return (
    <Modal
      open
      onClose={onClose}
      title={kind === "service" ? "New service account" : "Add a person"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="user-form" loading={create.isPending} disabled={!displayName.trim() || (kind === "human" && !email.trim())}>
            Create
          </Button>
        </>
      }
    >
      <form
        id="user-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <div role="radiogroup" aria-label="Account type" className="grid grid-cols-2 gap-2">
          {(
            [
              ["human", UserRound, "Person", "Someone who signs in with single sign-on or a password."],
              ["service", Bot, "Service account", "For CI, bots and the CLI; uses tokens."],
            ] as const
          ).map(([k, Icon, label, hint]) => (
            <button
              key={k}
              type="button"
              role="radio"
              aria-checked={kind === k}
              onClick={() => {
                setKind(k);
                if (k === "service" && orgRole === "admin") setOrgRole("member");
              }}
              className={cn("flex gap-2.5 rounded-md border p-3 text-left", kind === k ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
            >
              <Icon className={cn("mt-0.5 size-4 shrink-0", kind === k ? "text-accent" : "text-muted")} />
              <span>
                <span className="block text-sm font-medium">{label}</span>
                <span className="block text-xs text-muted">{hint}</span>
              </span>
            </button>
          ))}
        </div>
        <Field label={kind === "service" ? "Name" : "Display name"} required error={fe.displayName}>
          {(id) => <Input id={id} autoFocus value={displayName} maxLength={200} onChange={(e) => setDisplayName(e.target.value)} placeholder={kind === "service" ? "CI deploy bot" : "Grace Hopper"} />}
        </Field>
        <Field
          label="Email"
          required={kind === "human"}
          error={fe.email}
          hint={kind === "service" ? "Optional; a placeholder address is generated." : method === "sso" ? "They are matched by this address when they first sign in." : "They sign in with this address."}
        >
          {(id) => <Input id={id} type="email" value={email} onChange={(e) => setEmail(e.target.value)} />}
        </Field>
        <Field label="Organisation role" error={fe.orgRole} hint={kind === "service" ? "Service accounts cannot be admins; give them project roles instead." : undefined}>
          {(id) => (
            <Select id={id} value={orgRole} onChange={(e) => setOrgRole(e.target.value as Schemas["OrgRole"])}>
              {kind === "human" && <option value="admin">Admin</option>}
              <option value="member">Member</option>
              <option value="guest">Guest</option>
            </Select>
          )}
        </Field>
        {kind === "human" && local && (
          <div className="flex flex-col gap-1.5">
            <p className="text-[13px] font-medium">How they sign in</p>
            <SignInChoice value={method} onChange={setMethod} sso={sso} />
          </div>
        )}
        {kind === "human" && (
          <div className="flex flex-col gap-1.5">
            <p className="text-[13px] font-medium">Groups</p>
            <GroupChoice value={groupIds} onChange={setGroupIds} />
          </div>
        )}
        {create.error && Object.keys(fe).length === 0 && <ErrorBox error={create.error} />}
      </form>
    </Modal>
  );
}

export function EditNameDialog({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(user.displayName);
  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH("/admin/users/{userId}", { params: { path: { userId: user.id } }, body: { displayName: name.trim() } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["admin-users"] });
      toast.success("Name updated");
      onClose();
    },
  });
  return (
    <Modal
      open
      onClose={onClose}
      title={`Rename ${user.displayName}`}
      size="sm"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="name-form" loading={save.isPending} disabled={!name.trim() || name.trim() === user.displayName}>
            Save
          </Button>
        </>
      }
    >
      <form
        id="name-form"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
        className="flex flex-col gap-3"
      >
        <Field label="Display name">{(id) => <Input id={id} autoFocus value={name} onChange={(e) => setName(e.target.value)} />}</Field>
        {save.error && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}

/** ServiceTokensDialog lists and issues tokens for a service account. */
export function ServiceTokensDialog({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [creating, setCreating] = useState(false);
  const key = ["user-tokens", user.id];
  const tokens = useQuery({ queryKey: key, queryFn: () => unwrap(api.GET("/admin/users/{userId}/tokens", { params: { path: { userId: user.id } } })) });
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/admin/tokens/{tokenId}", { params: { path: { tokenId: id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: key });
      toast.success("Token revoked");
    },
    onError: toast.error,
  });
  return (
    <>
      <Modal
        open={!creating}
        onClose={onClose}
        title={`Tokens for ${user.displayName}`}
        size="xl"
        footer={
          <>
            <Button onClick={onClose}>Close</Button>
            <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
              New token
            </Button>
          </>
        }
      >
        <p className="mb-3 text-sm text-muted">The service account acts with its own project roles. Grant it access by adding it to projects (Members) or groups.</p>
        {tokens.error && <ErrorBox error={tokens.error} />}
        <div className="rounded-md border border-line">{tokens.isLoading ? <PageSpinner /> : <TokenTable tokens={tokens.data ?? []} onRevoke={(t) => revoke.mutate(t.id)} />}</div>
      </Modal>
      <CreateTokenDialog
        open={creating}
        onClose={() => setCreating(false)}
        title={`New token for ${user.displayName}`}
        allowAdmin={false}
        create={async (body) => {
          const t = await unwrap(api.POST("/admin/users/{userId}/tokens", { params: { path: { userId: user.id } }, body }));
          qc.invalidateQueries({ queryKey: key });
          return t;
        }}
        after={(token) => <CodeBlock>{`# e.g. as CI secrets\nWFM_SERVER=${window.location.origin}\nWFM_TOKEN=${token}`}</CodeBlock>}
      />
    </>
  );
}
