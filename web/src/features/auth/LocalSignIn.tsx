import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, LogIn } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Field, Input, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { MIN_PASSWORD, NewPasswordField, PasswordInput } from "./PasswordFields";
import { CodeInput, RecoveryCodesPanel, TotpSetupPanel, useOnce } from "./TwoFactor";

type Result = Schemas["LocalLoginResult"];
type Step = "password" | Exclude<Result["status"], "signed_in"> | "recovery_codes";

// A wrong password or code is a 401 these forms show, not an expired session (see main.tsx).
const signInMeta = { signIn: true };

/**
 * LocalSignIn signs in with an email address and password, then walks through whatever the
 * account needs next: a two-factor code, replacing a temporary password, or setting up
 * two-factor authentication when the organisation requires it.
 */
export function LocalSignIn({ returnTo, initialEmail = "" }: { returnTo: string; initialEmail?: string }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [step, setStep] = useState<Step>("password");
  const [email, setEmail] = useState(initialEmail);
  const [password, setPassword] = useState("");
  const [notice, setNotice] = useState<unknown>(null);
  const [codes, setCodes] = useState<string[]>([]);
  const [me, setMe] = useState<Schemas["Me"]>();
  const [name, setName] = useState<string>();

  const finish = (m: Schemas["Me"]) => {
    qc.setQueryData(["me"], m);
    navigate({ to: returnTo });
  };
  const handle = (r: Result) => {
    setNotice(null);
    if (r.status !== "signed_in") {
      if (r.displayName) setName(r.displayName);
      setStep(r.status);
      return;
    }
    if (r.recoveryCodes?.length) {
      setCodes(r.recoveryCodes);
      setMe(r.me);
      setStep("recovery_codes");
      return;
    }
    if (r.me) finish(r.me);
  };
  // An expired or abandoned second step starts over at the password.
  const onError = (e: unknown) => {
    if (e instanceof ApiError && e.code === "sign_in_expired") {
      setStep("password");
      setPassword("");
      setNotice(e);
    }
  };

  const login = useMutation({ mutationFn: () => unwrap(api.POST("/auth/local/login", { body: { email, password } })), onSuccess: handle, meta: signInMeta });
  const restart = () => {
    setStep("password");
    setPassword("");
    login.reset();
  };

  if (step === "totp_required") return <TotpStep onResult={handle} onError={onError} onBack={restart} />;
  if (step === "password_change_required") return <NewPasswordStep email={email} name={name} onResult={handle} onError={onError} onBack={restart} />;
  if (step === "totp_enrollment_required") return <EnrolStep onResult={handle} onError={onError} onBack={restart} />;
  if (step === "recovery_codes") return <RecoveryCodesPanel codes={codes} doneLabel="Continue" onDone={() => me && finish(me)} />;

  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        login.mutate();
      }}
    >
      {notice ? <ErrorBox error={notice} /> : null}
      <Field label="Email">
        {(id) => <Input id={id} type="email" required autoComplete="username" autoFocus={!initialEmail} value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />}
      </Field>
      <div className="flex flex-col gap-1.5">
        <div className="flex items-baseline justify-between">
          <label htmlFor="local-password" className="text-[13px] font-medium text-ink">
            Password
          </label>
          <Link to="/forgot-password" search={{ email: email || undefined }} className="text-xs font-medium text-accent hover:underline">
            Forgot password?
          </Link>
        </div>
        <PasswordInput id="local-password" required autoComplete="current-password" autoFocus={!!initialEmail} value={password} onChange={(e) => setPassword(e.target.value)} />
      </div>
      {login.error ? <ErrorBox error={login.error} /> : null}
      <Button type="submit" variant="primary" loading={login.isPending} disabled={!email || !password} icon={<LogIn className="size-4" />}>
        Sign in
      </Button>
    </form>
  );
}

function StepHeader({ title, body, onBack }: { title: string; body: string; onBack: () => void }) {
  return (
    <div className="mb-1 flex flex-col gap-1">
      <button type="button" onClick={onBack} className="mb-1 inline-flex w-fit items-center gap-1 text-xs font-medium text-muted hover:text-ink">
        <ArrowLeft className="size-3.5" /> Start over
      </button>
      <h2 className="text-base font-semibold">{title}</h2>
      <p className="text-sm text-muted">{body}</p>
    </div>
  );
}

type StepProps = { onResult: (r: Result) => void; onError: (e: unknown) => void; onBack: () => void };

function TotpStep({ onResult, onError, onBack }: StepProps) {
  const [code, setCode] = useState("");
  const [recovery, setRecovery] = useState(false);
  const verify = useMutation({ mutationFn: () => unwrap(api.POST("/auth/local/login/totp", { body: { code } })), onSuccess: onResult, onError, meta: signInMeta });
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        verify.mutate();
      }}
    >
      <StepHeader
        title="Two-factor authentication"
        body={recovery ? "Enter one of the recovery codes you saved when you set up two-factor authentication." : "Enter the 6-digit code from your authenticator app."}
        onBack={onBack}
      />
      <Field label={recovery ? "Recovery code" : "Authentication code"}>{(id) => <CodeInput key={String(recovery)} id={id} value={code} onChange={setCode} recovery={recovery} autoFocus />}</Field>
      {verify.error ? <ErrorBox error={verify.error} /> : null}
      <Button type="submit" variant="primary" loading={verify.isPending} disabled={recovery ? code.trim().length < 10 : code.length !== 6}>
        Verify
      </Button>
      <button
        type="button"
        className="text-xs font-medium text-accent hover:underline"
        onClick={() => {
          setRecovery(!recovery);
          setCode("");
          verify.reset();
        }}
      >
        {recovery ? "Use your authenticator app instead" : "Lost your device? Use a recovery code"}
      </button>
    </form>
  );
}

function NewPasswordStep({ email, name, onResult, onError, onBack }: StepProps & { email: string; name?: string }) {
  const [pw, setPw] = useState("");
  const save = useMutation({ mutationFn: () => unwrap(api.POST("/auth/local/login/new-password", { body: { newPassword: pw } })), onSuccess: onResult, onError, meta: signInMeta });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <StepHeader title="Choose a new password" body="You signed in with a temporary password. Choose your own to continue." onBack={onBack} />
      <Field label="New password" error={undefined}>
        {(id) => <NewPasswordField id={id} value={pw} onChange={setPw} email={email} name={name} error={fe.newPassword} autoFocus />}
      </Field>
      {save.error && !fe.newPassword ? <ErrorBox error={save.error} /> : null}
      <Button type="submit" variant="primary" loading={save.isPending} disabled={pw.length < MIN_PASSWORD}>
        Save and continue
      </Button>
    </form>
  );
}

function EnrolStep({ onResult, onError, onBack }: StepProps) {
  const setup = useMutation({ mutationFn: () => unwrap(api.POST("/auth/local/login/totp-setup")), onError, meta: signInMeta });
  const confirm = useMutation({ mutationFn: (code: string) => unwrap(api.POST("/auth/local/login/totp-setup/confirm", { body: { code } })), onSuccess: onResult, onError, meta: signInMeta });
  useOnce(setup.mutate);
  return (
    <div className="flex flex-col gap-3">
      <StepHeader title="Set up two-factor authentication" body="Your organisation requires a second step when you sign in with a password. It takes a minute." onBack={onBack} />
      {setup.error ? <ErrorBox error={setup.error} /> : null}
      {setup.data ? <TotpSetupPanel setup={setup.data} onConfirm={(c) => confirm.mutate(c)} pending={confirm.isPending} error={confirm.error} submitLabel="Turn on and continue" /> : !setup.error && <PageSpinner />}
    </div>
  );
}
