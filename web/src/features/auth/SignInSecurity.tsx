import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, ListRestart, ShieldCheck, ShieldOff } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { providersQuery, useMe } from "@/api/queries";
import { Badge, Button, Field, Input, Modal, PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, SettingsSection } from "@/features/settings/common";
import { relativeTime } from "@/lib/format";
import { MIN_PASSWORD, NewPasswordField, PasswordInput } from "./PasswordFields";
import { CodeInput, RecoveryCodesPanel, TotpSetupPanel, useOnce } from "./TwoFactor";

const securityKey = ["my-security"];

/** SignInSecurity is the Profile section for password and two-factor settings. */
export function SignInSecurity() {
  const { data: me } = useMe();
  const { data: providers } = useQuery(providersQuery);
  const sec = useQuery({ queryKey: securityKey, queryFn: () => unwrap(api.GET("/me/security")) });
  const [dialog, setDialog] = useState<"password" | "setup" | "disable" | "codes" | null>(null);
  const close = () => setDialog(null);
  const s = sec.data;
  const sso = (providers?.providers ?? []).map((p) => p.name).join(" or ");

  return (
    <SettingsSection title="Sign-in and security" description="How you sign in, and the second step that protects your password.">
      {sec.error && <ErrorBox error={sec.error} className="m-4" />}
      {!s ? (
        !sec.error && <PageSpinner />
      ) : !s.localAccount ? (
        <div className="flex items-start gap-3 px-5 py-4 text-sm">
          <KeyRound className="mt-0.5 size-4 shrink-0 text-faint" />
          <p className="text-muted">
            You sign in with single sign-on{sso ? ` (${sso})` : ""}. Your password and two-factor authentication are managed by your identity provider.
          </p>
        </div>
      ) : (
        <>
          <ul className="divide-y divide-line">
            <Row
              icon={<KeyRound className="size-4" />}
              title="Password"
              detail={s.passwordChangedAt ? `Last changed ${relativeTime(s.passwordChangedAt)}.` : "Set."}
              actions={
                <Button size="sm" onClick={() => setDialog("password")}>
                  Change password
                </Button>
              }
            />
            <Row
              icon={<ShieldCheck className="size-4" />}
              title={
                <>
                  Two-factor authentication {s.totpEnabled ? <Badge tone="success">On</Badge> : <Badge tone={s.require2fa ? "warning" : "neutral"}>Off</Badge>}
                </>
              }
              detail={
                s.totpEnabled
                  ? `A code from your authenticator app is needed after your password. ${s.recoveryCodesRemaining} of 10 recovery codes left.`
                  : "Add a code from an authenticator app on your phone to every password sign-in."
              }
              actions={
                s.totpEnabled ? (
                  <>
                    <Button size="sm" icon={<ListRestart className="size-3.5" />} onClick={() => setDialog("codes")}>
                      New recovery codes
                    </Button>
                    <Button size="sm" icon={<ShieldOff className="size-3.5" />} disabled={s.require2fa} title={s.require2fa ? "Required by your organisation" : undefined} onClick={() => setDialog("disable")}>
                      Turn off
                    </Button>
                  </>
                ) : (
                  <Button size="sm" variant="primary" onClick={() => setDialog("setup")}>
                    Set up
                  </Button>
                )
              }
            />
          </ul>
          {s.require2fa && !s.totpEnabled && (
            <div className="border-t border-line p-4">
              <Callout tone="warning" title="Required by your organisation">
                You'll be asked to set up two-factor authentication the next time you sign in.
              </Callout>
            </div>
          )}
        </>
      )}
      {dialog === "password" && me && <ChangePasswordDialog email={me.email} name={me.displayName} onClose={close} />}
      {dialog === "setup" && <SetupDialog onClose={close} />}
      {dialog === "disable" && <DisableDialog onClose={close} />}
      {dialog === "codes" && <NewCodesDialog onClose={close} />}
    </SettingsSection>
  );
}

function Row({ icon, title, detail, actions }: { icon: ReactNode; title: ReactNode; detail: ReactNode; actions: ReactNode }) {
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-4">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-2 text-muted">{icon}</span>
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-2 text-sm font-medium">{title}</p>
        <p className="text-[13px] text-muted">{detail}</p>
      </div>
      <div className="flex flex-wrap gap-2">{actions}</div>
    </li>
  );
}

function ChangePasswordDialog({ email, name, onClose }: { email: string; name: string; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const save = useMutation({
    mutationFn: () => unwrap(api.POST("/me/password", { body: { currentPassword: current, newPassword: next } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: securityKey });
      toast.success("Password changed. You were signed out on your other devices.");
      onClose();
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  return (
    <Modal
      open
      onClose={onClose}
      title="Change password"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" form="password-form" variant="primary" loading={save.isPending} disabled={!current || next.length < MIN_PASSWORD}>
            Change password
          </Button>
        </>
      }
    >
      <form
        id="password-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <input type="email" name="username" autoComplete="username" value={email} readOnly hidden />
        <Field label="Current password" error={fe.currentPassword}>
          {(id) => <PasswordInput id={id} autoComplete="current-password" autoFocus value={current} onChange={(e) => setCurrent(e.target.value)} invalid={!!fe.currentPassword} />}
        </Field>
        <Field label="New password">{(id) => <NewPasswordField id={id} value={next} onChange={setNext} email={email} name={name} error={fe.newPassword} />}</Field>
        {save.error && !fe.currentPassword && !fe.newPassword ? <ErrorBox error={save.error} /> : null}
        <p className="text-xs text-muted">Changing your password signs you out everywhere except here, and we'll email you to confirm.</p>
      </form>
    </Modal>
  );
}

function SetupDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const setup = useMutation({ mutationFn: () => unwrap(api.POST("/me/totp/setup")) });
  const confirm = useMutation({
    mutationFn: (code: string) => unwrap(api.POST("/me/totp/confirm", { body: { code } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKey }),
  });
  useOnce(setup.mutate);
  return (
    <Modal open onClose={onClose} title={confirm.data ? "Two-factor authentication is on" : "Set up two-factor authentication"} size="lg">
      {confirm.data ? (
        <RecoveryCodesPanel
          codes={confirm.data.codes}
          onDone={() => {
            toast.success("Two-factor authentication is on");
            onClose();
          }}
        />
      ) : setup.error ? (
        <ErrorBox error={setup.error} />
      ) : setup.data ? (
        <TotpSetupPanel setup={setup.data} onConfirm={(c) => confirm.mutate(c)} pending={confirm.isPending} error={confirm.error} footer={<Button onClick={onClose}>Cancel</Button>} />
      ) : (
        <PageSpinner />
      )}
    </Modal>
  );
}

function DisableDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const off = useMutation({
    mutationFn: () => unwrap(api.POST("/me/totp/disable", { body: { password, code } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: securityKey });
      toast.success("Two-factor authentication is off");
      onClose();
    },
  });
  const fe = off.error instanceof ApiError ? off.error.fieldErrors() : {};
  return (
    <Modal
      open
      onClose={onClose}
      title="Turn off two-factor authentication?"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" form="totp-off-form" variant="danger" loading={off.isPending} disabled={!password || !code}>
            Turn off
          </Button>
        </>
      }
    >
      <form
        id="totp-off-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          off.mutate();
        }}
      >
        <p className="text-sm text-muted">Anyone who learns your password could then sign in as you. Confirm with your password and a code.</p>
        <Field label="Password" error={fe.password}>
          {(id) => <PasswordInput id={id} autoComplete="current-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />}
        </Field>
        <Field label="Authentication or recovery code" error={fe.code}>
          {(id) => <Input id={id} autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" />}
        </Field>
        {off.error && !fe.password && !fe.code ? <ErrorBox error={off.error} /> : null}
      </form>
    </Modal>
  );
}

function NewCodesDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const regen = useMutation({
    mutationFn: () => unwrap(api.POST("/me/totp/recovery-codes", { body: { code } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: securityKey }),
  });
  const fe = regen.error instanceof ApiError ? regen.error.fieldErrors() : {};
  return (
    <Modal open onClose={onClose} title="New recovery codes" size={regen.data ? "lg" : "md"}>
      {regen.data ? (
        <RecoveryCodesPanel codes={regen.data.codes} onDone={onClose} />
      ) : (
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            regen.mutate();
          }}
        >
          <p className="text-sm text-muted">Your current recovery codes stop working. Enter a code from your authenticator app to continue.</p>
          <Field label="Authentication code" error={fe.code}>
            {(id) => <CodeInput id={id} value={code} onChange={setCode} autoFocus />}
          </Field>
          {regen.error && !fe.code ? <ErrorBox error={regen.error} /> : null}
          <div className="flex justify-end gap-2">
            <Button onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" loading={regen.isPending} disabled={code.length !== 6}>
              Create new codes
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
