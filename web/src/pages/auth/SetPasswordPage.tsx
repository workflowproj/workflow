import { useState } from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { LinkIcon } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, Field, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { AuthShell } from "@/features/auth/AuthShell";
import { MIN_PASSWORD, NewPasswordField } from "@/features/auth/PasswordFields";
import { formatDateTime } from "@/lib/format";

const invite = getRouteApi("/invite");
const reset = getRouteApi("/reset-password");

export function InvitePage() {
  return <SetPassword token={invite.useSearch().token ?? ""} expected="invite" />;
}

export function ResetPasswordPage() {
  return <SetPassword token={reset.useSearch().token ?? ""} expected="reset" />;
}

/** SetPassword lets the holder of an invitation or reset link choose a password. */
function SetPassword({ token, expected }: { token: string; expected: "invite" | "reset" }) {
  const navigate = useNavigate();
  const [pw, setPw] = useState("");
  const link = useQuery({
    queryKey: ["local-link", token],
    queryFn: () => unwrap(api.POST("/auth/local/link", { body: { token } })),
    enabled: token !== "",
    retry: false,
    staleTime: Infinity,
  });
  const save = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/local/link/redeem", { body: { token, password: pw } })),
    onSuccess: (l) => navigate({ to: "/login", search: { notice: "password_set", email: l.email } }),
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const signIn = (
    <Link to="/login" className="font-medium text-accent hover:underline">
      Back to sign in
    </Link>
  );

  if (token === "" || link.error) {
    return (
      <AuthShell title={expected === "invite" ? "Invitation unavailable" : "Reset link unavailable"} below={signIn}>
        <div className="flex flex-col items-center gap-3 py-2 text-center">
          <span className="flex size-10 items-center justify-center rounded-full bg-warning-soft text-warning">
            <LinkIcon className="size-5" />
          </span>
          <p className="text-sm">{link.error instanceof ApiError && link.error.code !== "invalid_link" ? link.error.message : "This link is invalid, has already been used or has expired."}</p>
          <p className="text-[13px] text-muted">
            {expected === "invite" ? (
              "Ask your administrator to send a new invitation."
            ) : (
              <>
                <Link to="/forgot-password" className="font-medium text-accent hover:underline">
                  Request a new link
                </Link>{" "}
                — reset links work for one hour.
              </>
            )}
          </p>
        </div>
      </AuthShell>
    );
  }
  if (!link.data) {
    return (
      <AuthShell title={expected === "invite" ? "Welcome to Workflow" : "Choose a new password"}>
        <PageSpinner />
      </AuthShell>
    );
  }
  const l = link.data;
  const welcome = l.purpose === "invite";
  return (
    <AuthShell
      title={welcome ? `Welcome, ${l.displayName}` : "Choose a new password"}
      subtitle={welcome ? "Choose a password to finish setting up your account." : `For ${l.email}.`}
      below={signIn}
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        {welcome && (
          <dl className="grid grid-cols-[5rem_1fr] gap-y-1 rounded-md bg-surface-2 px-3 py-2 text-[13px]">
            <dt className="text-muted">Sign in as</dt>
            <dd className="font-medium break-all">{l.email}</dd>
            {l.expiresAt && (
              <>
                <dt className="text-muted">Link valid</dt>
                <dd>until {formatDateTime(l.expiresAt)}</dd>
              </>
            )}
          </dl>
        )}
        {/* Helps password managers save the new password against the right account. */}
        <input type="email" name="username" autoComplete="username" value={l.email} readOnly hidden />
        <Field label={welcome ? "Password" : "New password"}>{(id) => <NewPasswordField id={id} value={pw} onChange={setPw} email={l.email} name={l.displayName} error={fe.password} autoFocus />}</Field>
        {save.error && !fe.password ? <ErrorBox error={save.error} /> : null}
        <Button type="submit" variant="primary" loading={save.isPending} disabled={pw.length < MIN_PASSWORD}>
          {welcome ? "Set password" : "Save new password"}
        </Button>
        {!welcome && <p className="text-xs text-muted">Saving signs you out on every device. If two-factor authentication is on, you'll still need your code.</p>}
      </form>
    </AuthShell>
  );
}
