import { useState } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderKanban, KeyRound } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { providersQuery } from "@/api/queries";
import { Button, Field, Input, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";

const route = getRouteApi("/login");

const loginErrors: Record<string, string> = {
  domain_not_allowed: "Your account's domain is not allowed to sign in here.",
  not_provisioned: "Your account has not been set up yet. Ask an administrator to give you access.",
  deactivated: "Your account is deactivated.",
  email_unverified: "Your identity provider did not confirm your email address.",
  invalid_state: "The sign-in attempt expired. Please try again.",
  access_denied: "Sign-in was cancelled.",
  provider_unavailable: "The sign-in provider is unavailable right now.",
};

export function LoginPage() {
  const { error, returnTo } = route.useSearch();
  const { data, isLoading } = useQuery(providersQuery);
  const safeReturn = returnTo && returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/projects";

  return (
    <div className="flex min-h-full items-center justify-center bg-canvas p-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          <span className="flex size-11 items-center justify-center rounded-xl bg-accent text-accent-ink shadow-sm">
            <FolderKanban className="size-6" />
          </span>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Sign in to Workflow</h1>
            <p className="mt-1 text-sm text-muted">Plan, track and ship software together.</p>
          </div>
        </div>
        <div className="rounded-lg border border-line bg-surface p-5 shadow-sm">
          {error && <p className="mb-4 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">{loginErrors[error] ?? "Sign-in failed."}</p>}
          {isLoading ? (
            <PageSpinner />
          ) : (
            <div className="flex flex-col gap-2">
              {data?.providers.map((p) => (
                <a key={p.id} href={`${p.loginUrl}?returnTo=${encodeURIComponent(safeReturn)}`}>
                  <Button className="w-full" icon={<KeyRound className="size-4" />}>
                    Continue with {p.name}
                  </Button>
                </a>
              ))}
              {data && data.providers.length === 0 && !data.devLogin && <p className="text-sm text-muted">No sign-in method is configured. See the deployment guide.</p>}
              {data?.devLogin && <DevLogin returnTo={safeReturn} separated={data.providers.length > 0} />}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function DevLogin({ returnTo, separated }: { returnTo: string; separated: boolean }) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const qc = useQueryClient();
  const navigate = useNavigate();
  const login = useMutation({
    mutationFn: () => unwrap(api.POST("/auth/dev-login", { body: { email, displayName: name || undefined } })),
    onSuccess: (me) => {
      qc.setQueryData(["me"], me);
      navigate({ to: returnTo });
    },
  });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        login.mutate();
      }}
      className={separated ? "mt-3 border-t border-line pt-4" : ""}
    >
      <p className="mb-3 text-xs font-medium tracking-wide text-warning uppercase">Development sign-in</p>
      <div className="flex flex-col gap-3">
        <Field label="Email" required>
          {(id) => <Input id={id} type="email" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />}
        </Field>
        <Field label="Display name" hint="Used when the account is created.">
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} placeholder="Ada Lovelace" />}
        </Field>
        {login.error && <ErrorBox error={login.error} />}
        <Button type="submit" variant="primary" loading={login.isPending}>
          Sign in
        </Button>
      </div>
    </form>
  );
}
