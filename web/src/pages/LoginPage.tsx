import { useState } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { providersQuery } from "@/api/queries";
import { Button, Field, Input, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { AuthShell } from "@/features/auth/AuthShell";
import { LocalSignIn } from "@/features/auth/LocalSignIn";
import { Callout } from "@/features/settings/common";
import { cn } from "@/lib/cn";

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

type Method = "password" | "dev";
const methodKey = "wfm.signInMethod";

function storedMethod(): Method | null {
  try {
    const v = localStorage.getItem(methodKey);
    return v === "password" || v === "dev" ? v : null;
  } catch {
    return null;
  }
}

export function LoginPage() {
  const { error, returnTo, notice, email } = route.useSearch();
  const { data, isLoading } = useQuery(providersQuery);
  const [chosen, setChosen] = useState<Method | null>(storedMethod);
  const safeReturn = returnTo && returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/projects";
  const local = !!data?.localLogin;
  const dev = !!data?.devLogin;
  // Development sign-in stays the default where it is on, so local set-ups work as before.
  const method: Method = local && dev ? (chosen ?? "dev") : local ? "password" : "dev";
  const choose = (m: Method) => {
    setChosen(m);
    try {
      localStorage.setItem(methodKey, m);
    } catch {
      /* private mode */
    }
  };

  return (
    <AuthShell title="Sign in to Workflow" subtitle="Plan, track and ship software together.">
      {error && <p className="mb-4 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">{loginErrors[error] ?? "Sign-in failed."}</p>}
      {notice === "password_set" && (
        <Callout className="mb-4" title="Your password is set">
          Sign in with it to continue.
        </Callout>
      )}
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
          {data && data.providers.length === 0 && !local && !dev && <p className="text-sm text-muted">No sign-in method is configured. See the deployment guide.</p>}
          {data && data.providers.length > 0 && (local || dev) && (
            <div className="my-2 flex items-center gap-3 text-xs text-faint" aria-hidden>
              <span className="h-px flex-1 bg-line" />
              or
              <span className="h-px flex-1 bg-line" />
            </div>
          )}
          {local && dev && (
            <div role="tablist" aria-label="Sign-in method" className="mb-2 grid grid-cols-2 gap-1 rounded-md bg-surface-2 p-1">
              {(
                [
                  ["password", "Password"],
                  ["dev", "Development"],
                ] as const
              ).map(([m, label]) => (
                <button
                  key={m}
                  type="button"
                  role="tab"
                  aria-selected={method === m}
                  onClick={() => choose(m)}
                  className={cn("rounded px-2 py-1 text-[13px] font-medium", method === m ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink")}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
          {local && method === "password" && <LocalSignIn returnTo={safeReturn} initialEmail={email} />}
          {dev && method === "dev" && <DevLogin returnTo={safeReturn} />}
        </div>
      )}
    </AuthShell>
  );
}

function DevLogin({ returnTo }: { returnTo: string }) {
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
