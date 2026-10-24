import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, LockOpen, Mail, MailX, ShieldCheck, ShieldOff, Trash2, UserRound } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { providersQuery } from "@/api/queries";
import { Badge, Button, Checkbox, MenuItem, MenuLabel, Modal, useConfirm, useToast } from "@/components/ui";
import { isDirectoryManaged } from "@/features/admin/badges";
import { SecretOnce } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { relativeTime } from "@/lib/format";

type AdminUser = Schemas["AdminUser"];

/** canHavePassword: people not managed by SCIM or Google directory sync. */
export function canHavePassword(u: AdminUser) {
  return u.kind === "human" && !isDirectoryManaged(u.source);
}

/**
 * SignInSummary is a one-line note under the source badge for local accounts: invited (and
 * until when), or "Local account" with the two-factor state and a pending temporary password.
 */
export function SignInSummary({ user }: { user: AdminUser }) {
  const l = user.localAccount;
  if (!l) return null;
  if (l.status === "invited") {
    const expired = !l.invitationExpiresAt || new Date(l.invitationExpiresAt) < new Date();
    return (
      <p className="mt-1 text-[11px] whitespace-nowrap">
        <span className="font-semibold text-warning">Invited</span>
        <span className={expired ? "text-danger" : "text-muted"}> · {expired ? "link expired" : `expires ${relativeTime(l.invitationExpiresAt)}`}</span>
      </p>
    );
  }
  return (
    <p className="mt-1 flex items-center gap-1 text-[11px] whitespace-nowrap text-muted" title="Signs in with an email address and password">
      <KeyRound className="size-3 shrink-0" />
      <span className="font-medium text-ink">Local account</span>·
      {l.totpEnabled ? (
        <span className="flex items-center gap-0.5 font-medium text-success" title="Two-factor authentication is on">
          <ShieldCheck className="size-3" />
          2FA
        </span>
      ) : (
        <span>no 2FA</span>
      )}
      {l.status === "password_change_required" && <span title="A temporary password is set; they choose their own at the next sign-in">· temp. password</span>}
    </p>
  );
}

/** LockedBadge marks an account locked after repeated failed sign-ins. */
export function LockedBadge({ user }: { user: AdminUser }) {
  const until = user.localAccount?.lockedUntil;
  if (!until || new Date(until) < new Date()) return null;
  return (
    <span title={`Locked after repeated failed sign-ins until ${new Date(until).toLocaleTimeString()}. Unlock it from the actions menu.`}>
      <Badge tone="danger">Locked</Badge>
    </span>
  );
}

type Action = "invite" | "revoke" | "temporary" | "reset" | "unlock" | "resetTotp" | "removePassword";

async function perform(user: AdminUser, action: Action): Promise<string | null> {
  const params = { path: { userId: user.id } };
  switch (action) {
    case "invite":
      await unwrap(api.POST("/admin/users/{userId}/invitation", { params }));
      return null;
    case "revoke":
      await unwrap(api.DELETE("/admin/users/{userId}/invitation", { params }));
      return null;
    case "temporary":
      return (await unwrap(api.POST("/admin/users/{userId}/temporary-password", { params }))).password;
    case "reset":
      await unwrap(api.POST("/admin/users/{userId}/password-reset", { params }));
      return null;
    case "unlock":
      await unwrap(api.POST("/admin/users/{userId}/unlock", { params }));
      return null;
    case "resetTotp":
      await unwrap(api.DELETE("/admin/users/{userId}/totp", { params }));
      return null;
    case "removePassword":
      await unwrap(api.DELETE("/admin/users/{userId}/password", { params }));
      return null;
  }
}

const done: Record<Exclude<Action, "temporary">, (u: AdminUser) => string> = {
  invite: (u) => `Invitation sent to ${u.email}`,
  revoke: () => "Invitation revoked",
  reset: (u) => `Password reset link sent to ${u.email}`,
  unlock: (u) => `${u.displayName} can sign in again`,
  resetTotp: (u) => `Two-factor authentication turned off for ${u.displayName}`,
  removePassword: (u) => `${u.displayName} now signs in with single sign-on only`,
};

/**
 * useLocalAccountActions gives the user list its password actions: `menu(user, close)` renders
 * the menu items for a person and `dialogs` the temporary-password result.
 */
export function useLocalAccountActions() {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const { data: providers } = useQuery(providersQuery);
  const enabled = !!providers?.localLogin;
  const [temp, setTemp] = useState<{ user: AdminUser; password: string } | null>(null);
  const run = useMutation({
    mutationFn: ({ user, action }: { user: AdminUser; action: Action }) => perform(user, action),
    onSuccess: (password, { user, action }) => {
      qc.invalidateQueries({ queryKey: ["admin-users"] });
      if (action === "temporary" && password) setTemp({ user, password });
      else if (action !== "temporary") toast.success(done[action](user));
    },
    onError: toast.error,
  });
  const go = async (user: AdminUser, action: Action, ask?: { title: string; body: ReactNode; confirmLabel: string; danger?: boolean }) => {
    if (ask && !(await confirm(ask))) return;
    run.mutate({ user, action });
  };

  const menu = (u: AdminUser, close: () => void) => {
    if (!enabled || !canHavePassword(u)) return null;
    const l = u.localAccount;
    const item = (action: Action, icon: ReactNode, label: string, ask?: Parameters<typeof go>[2], danger?: boolean) => (
      <MenuItem
        key={action}
        icon={icon}
        danger={danger}
        disabled={run.isPending || (!u.active && action !== "removePassword" && action !== "revoke" && action !== "resetTotp")}
        onClick={() => {
          close();
          go(u, action, ask);
        }}
      >
        {label}
      </MenuItem>
    );
    const tempAsk = l?.status === "active" ? { title: `Set a temporary password for ${u.displayName}?`, body: "Their current password stops working and they are signed out everywhere. They choose a new one at their next sign-in.", confirmLabel: "Set temporary password" } : undefined;
    return (
      <>
        <div className="my-1 border-t border-line" />
        <MenuLabel>Password sign-in</MenuLabel>
        {!l && item("invite", <Mail className="size-4" />, "Send invitation")}
        {l?.status === "invited" && item("invite", <Mail className="size-4" />, "Resend invitation")}
        {l?.status === "invited" && item("revoke", <MailX className="size-4" />, "Revoke invitation")}
        {l && l.status !== "invited" && item("reset", <Mail className="size-4" />, "Send password reset")}
        {item("temporary", <KeyRound className="size-4" />, "Set temporary password", tempAsk)}
        {l?.lockedUntil && new Date(l.lockedUntil) > new Date() && item("unlock", <LockOpen className="size-4" />, "Unlock")}
        {l?.totpEnabled &&
          item("resetTotp", <ShieldOff className="size-4" />, "Reset two-factor", {
            title: `Turn off two-factor authentication for ${u.displayName}?`,
            body: "Do this when they have lost their device. They can set it up again from their profile (or must, at their next sign-in, if your organisation requires it).",
            confirmLabel: "Reset two-factor",
          })}
        {l &&
          l.status !== "invited" &&
          item(
            "removePassword",
            <Trash2 className="size-4" />,
            "Remove password",
            {
              title: `Remove ${u.displayName}'s password?`,
              body: "They are signed out and can then only sign in with single sign-on. Two-factor settings and recovery codes are removed too.",
              confirmLabel: "Remove password",
              danger: true,
            },
            true,
          )}
      </>
    );
  };

  const dialogs = temp && <TemporaryPasswordDialog user={temp.user} password={temp.password} onClose={() => setTemp(null)} />;
  return { enabled, menu, dialogs };
}

export function TemporaryPasswordDialog({ user, password, onClose }: { user: Pick<AdminUser, "displayName" | "email">; password: string; onClose: () => void }) {
  return (
    <Modal
      open
      onClose={onClose}
      title={`Temporary password for ${user.displayName}`}
      footer={
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      }
    >
      <SecretOnce label="Temporary password" value={password}>
        <p className="text-[13px] text-muted">
          Give it to {user.displayName} through a channel you trust. They sign in as <span className="font-medium text-ink">{user.email}</span> and must choose their own password straight away.
        </p>
      </SecretOnce>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Adding a person

export type SignInMethod = "sso" | "invite" | "temporary";

/** SignInChoice picks how a new person will sign in. */
export function SignInChoice({ value, onChange, sso }: { value: SignInMethod; onChange: (m: SignInMethod) => void; sso: boolean }) {
  const options: [SignInMethod, string, string][] = [
    ["invite", "Email invitation", "They choose a password from a link (valid 7 days)."],
    ["temporary", "Temporary password", "You pass it on; they change it at first sign-in."],
  ];
  if (sso) options.unshift(["sso", "Single sign-on", "They sign in with your identity provider."]);
  return (
    <div role="radiogroup" aria-label="How they sign in" className={cn("grid gap-2", options.length === 3 ? "sm:grid-cols-3" : "grid-cols-2")}>
      {options.map(([m, label, hint]) => (
        <button
          key={m}
          type="button"
          role="radio"
          aria-checked={value === m}
          onClick={() => onChange(m)}
          className={cn("rounded-md border p-2.5 text-left", value === m ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
        >
          <span className="block text-[13px] font-medium">{label}</span>
          <span className="block text-xs text-muted">{hint}</span>
        </button>
      ))}
    </div>
  );
}

/** GroupChoice selects local groups for a new person. */
export function GroupChoice({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const groups = useQuery({ queryKey: ["groups", ""], queryFn: () => unwrap(api.GET("/groups")) });
  const local = (groups.data ?? []).filter((g) => g.source === "local");
  if (groups.isLoading) return null;
  if (local.length === 0) return <p className="text-xs text-muted">No local groups yet; create them under Groups.</p>;
  return (
    <div className="flex max-h-36 flex-col gap-1.5 overflow-y-auto rounded-md border border-line p-2.5">
      {local.map((g) => (
        <Checkbox
          key={g.id}
          label={
            <span className="flex items-center gap-1.5">
              <UserRound className="size-3.5 text-faint" />
              {g.displayName}
              {g.orgRole && <span className="text-xs text-muted">(org {g.orgRole})</span>}
            </span>
          }
          checked={value.includes(g.id)}
          onChange={(e) => onChange(e.target.checked ? [...value, g.id] : value.filter((id) => id !== g.id))}
        />
      ))}
    </div>
  );
}

/** startSignIn sends the invitation or sets the temporary password for a new person. */
export async function startSignIn(userId: string, method: SignInMethod): Promise<string | null> {
  const params = { path: { userId } };
  if (method === "invite") await unwrap(api.POST("/admin/users/{userId}/invitation", { params }));
  if (method === "temporary") return (await unwrap(api.POST("/admin/users/{userId}/temporary-password", { params }))).password;
  return null;
}
