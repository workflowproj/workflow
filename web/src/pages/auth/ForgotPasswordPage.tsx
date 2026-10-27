import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { MailCheck } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { providersQuery } from "@/api/queries";
import { Button, Field, Input } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { AuthShell } from "@/features/auth/AuthShell";

const route = getRouteApi("/forgot-password");

export function ForgotPasswordPage() {
  const search = route.useSearch();
  const [email, setEmail] = useState(search.email ?? "");
  const providers = useQuery(providersQuery);
  const send = useMutation({ mutationFn: () => unwrap(api.POST("/auth/local/forgot-password", { body: { email } })) });
  const back = (
    <Link to="/login" search={{ email: email || undefined }} className="font-medium text-accent hover:underline">
      Back to sign in
    </Link>
  );

  if (send.isSuccess) {
    return (
      <AuthShell title="Check your email" below={back}>
        <div className="flex flex-col items-center gap-3 py-2 text-center">
          <span className="flex size-10 items-center justify-center rounded-full bg-success-soft text-success">
            <MailCheck className="size-5" />
          </span>
          <p className="text-sm">
            If <span className="font-medium">{email}</span> has a password here, we've sent it a link to choose a new one.
          </p>
          <p className="text-[13px] text-muted">The link works once, for one hour. Nothing arrived? Check your spam folder, or ask your administrator — people who sign in with single sign-on reset their password with their identity provider.</p>
        </div>
      </AuthShell>
    );
  }
  // Without an email server a reset link reaches nobody, so don't offer to send one.
  if (providers.data && !providers.data.emailDelivery) {
    return (
      <AuthShell title="Reset your password" below={back}>
        <p className="text-sm">This server doesn't send email, so it can't send you a reset link.</p>
        <p className="mt-2 text-[13px] text-muted">Ask an administrator to set a new temporary password for you. People who sign in with single sign-on reset their password with their identity provider.</p>
      </AuthShell>
    );
  }
  return (
    <AuthShell title="Reset your password" subtitle="We'll email you a link to choose a new one." below={back}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          send.mutate();
        }}
      >
        <Field label="Email">{(id) => <Input id={id} type="email" required autoFocus autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />}</Field>
        {send.error ? <ErrorBox error={send.error} /> : null}
        <Button type="submit" variant="primary" loading={send.isPending} disabled={!email.includes("@")}>
          Send reset link
        </Button>
      </form>
    </AuthShell>
  );
}
