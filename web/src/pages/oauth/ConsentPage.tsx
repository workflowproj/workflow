import { useEffect, useRef, useState, type ReactNode } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { BookOpen, CircleAlert, FolderKanban, Laptop, Link2, PencilLine, ShieldCheck } from "lucide-react";
import { api, ApiError, errorMessage, unwrap, type Schemas } from "@/api/client";
import { useMe } from "@/api/queries";
import { Button, PageSpinner, Spinner } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { Callout } from "@/features/settings/common";
import { AppIcon, canWrite, type OAuthScope } from "@/features/settings/connected-apps/ConnectedApps";
import { ThemeMenu } from "@/layout/ThemeMenu";
import { cn } from "@/lib/cn";

/*
 * The OAuth consent page. /oauth/authorize validates an AI assistant's request and sends the
 * browser to /oauth/consent/<sealed request> (after sign-in if needed). Approving or denying
 * returns the browser to the app's registered redirect URI; the API builds that URL, so nothing
 * here can redirect anywhere else. Errors found before the app's redirect URI was validated are
 * shown at /oauth/consent?error=<code>.
 */

type Consent = Schemas["OAuthConsent"];

const requestRoute = getRouteApi("/oauth/consent/$request");
const errorRoute = getRouteApi("/oauth/consent");

function Frame({ children }: { children: ReactNode }) {
  return (
    <div className="relative flex min-h-full items-center justify-center bg-canvas p-4">
      <div className="absolute top-3 right-3">
        <ThemeMenu />
      </div>
      <div className="w-full max-w-md py-8">{children}</div>
    </div>
  );
}

function WorkflowMark({ size = 44 }: { size?: number }) {
  return (
    <span style={{ width: size, height: size }} className="flex shrink-0 items-center justify-center rounded-xl bg-accent text-accent-ink shadow-sm">
      <FolderKanban className="size-[55%]" />
    </span>
  );
}

const pageErrors: Record<string, { title: string; body: string }> = {
  invalid_client: {
    title: "Workflow doesn't know this app",
    body: "The app that sent you here isn't registered with Workflow, or its registration has expired. Go back to the app and connect again.",
  },
  invalid_client_metadata: {
    title: "Couldn't check this app",
    body: "Workflow couldn't load or accept the app's published details (its client metadata document), so it can't show you who is asking. Try again later, or contact the app's developer.",
  },
  invalid_redirect_uri: {
    title: "This request was stopped",
    body: "The app asked Workflow to send you to an address it hasn't registered. To protect your account, Workflow didn't send you anywhere. Go back to the app and connect again.",
  },
  invalid_request: {
    title: "This request isn't valid",
    body: "The connection request from the app was incomplete or malformed. Go back to the app and connect again.",
  },
};

function ErrorCard({ title, body }: { title: string; body: ReactNode }) {
  return (
    <Frame>
      <div className="mb-6 flex justify-center">
        <WorkflowMark />
      </div>
      <div role="alert" className="rounded-lg border border-line bg-surface p-6 shadow-sm">
        <div className="flex items-start gap-3">
          <CircleAlert className="mt-0.5 size-5 shrink-0 text-danger" />
          <div className="min-w-0">
            <h1 className="text-base font-semibold">{title}</h1>
            <p className="mt-1.5 text-sm text-muted">{body}</p>
          </div>
        </div>
        <div className="mt-5 flex justify-end">
          <a href="/projects">
            <Button>Go to Workflow</Button>
          </a>
        </div>
      </div>
    </Frame>
  );
}

/** ConsentErrorPage explains why an app could not be connected (/oauth/consent?error=…). */
export function ConsentErrorPage() {
  const { error } = errorRoute.useSearch();
  const known = error ? pageErrors[error] : undefined;
  if (!known) return <ErrorCard title="Nothing to approve" body="This page is where Workflow asks you before an app connects. Start connecting from the app itself." />;
  return <ErrorCard title={known.title} body={known.body} />;
}

/** ConsentPage asks the signed-in person whether an app may act for them. */
export function ConsentPage() {
  const { request } = requestRoute.useParams();
  const consent = useQuery({
    queryKey: ["oauth-consent", request],
    queryFn: () => unwrap(api.GET("/oauth/consent", { params: { query: { request } } })),
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  const decide = useMutation({
    mutationFn: (body: { approve: boolean; scopes?: OAuthScope[] }) => unwrap(api.POST("/oauth/consent", { body: { request, ...body } })),
    onSuccess: ({ redirectUrl }) => window.location.assign(redirectUrl),
  });
  // Returning after signing in, with an earlier approval that covers this request: continue.
  const auto = useRef(false);
  const data = consent.data;
  useEffect(() => {
    if (data?.alreadyGranted && !auto.current) {
      auto.current = true;
      decide.mutate({ approve: true, scopes: data.requestedScopes });
    }
  }, [data, decide]);

  if (consent.isLoading) {
    return (
      <Frame>
        <PageSpinner />
      </Frame>
    );
  }
  if (consent.error || !data) {
    const code = consent.error instanceof ApiError ? consent.error.code : undefined;
    return (
      <ErrorCard
        title={code === "expired" ? "This request has expired" : "Can't continue"}
        body={consent.error ? errorMessage(consent.error) : "Something went wrong. Go back to the app and connect again."}
      />
    );
  }
  if (decide.isPending || decide.isSuccess) {
    return <Returning consent={data} approved={decide.variables?.approve !== false} />;
  }
  return <ConsentForm consent={data} onDecide={(approve, scopes) => decide.mutate({ approve, scopes })} error={decide.error} />;
}

function Returning({ consent, approved }: { consent: Consent; approved: boolean }) {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setSlow(true), 2500);
    return () => clearTimeout(t);
  }, []);
  return (
    <Frame>
      <div className="flex flex-col items-center gap-4 rounded-lg border border-line bg-surface p-8 text-center shadow-sm" aria-live="polite">
        <AppIcon client={consent.client} size={44} />
        <div>
          <h1 className="text-base font-semibold">
            {approved ? "Connected" : "Not connected"} — returning to {consent.client.name}
          </h1>
          <p className="mt-1 text-sm text-muted">
            {slow
              ? `If ${consent.client.name} didn't open, switch back to it. You can close this tab.`
              : `Sending you back to ${consent.redirectHost}…`}
          </p>
        </div>
        {!slow && <Spinner className="size-5" />}
      </div>
    </Frame>
  );
}

function ConsentForm({ consent, onDecide, error }: { consent: Consent; onDecide: (approve: boolean, scopes?: OAuthScope[]) => void; error: unknown }) {
  const { data: me } = useMe();
  const app = consent.client;
  const offersWrite = canWrite(consent.requestedScopes);
  const [write, setWrite] = useState(offersWrite);
  const signOut = async () => {
    await api.POST("/auth/logout");
    window.location.assign(`/login?returnTo=${encodeURIComponent(window.location.pathname)}`);
  };

  return (
    <Frame>
      <div className="mb-6 flex flex-col items-center gap-4 text-center">
        <div className="flex items-center gap-3">
          <AppIcon client={app} size={44} className="shadow-sm" />
          <Link2 className="size-4 text-faint" aria-hidden />
          <WorkflowMark />
        </div>
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Connect {app.name} to Workflow</h1>
          <p className="mt-1 text-sm text-muted">{app.name} wants to work in Workflow as you.</p>
        </div>
      </div>

      <form
        className="rounded-lg border border-line bg-surface shadow-sm"
        onSubmit={(e) => {
          e.preventDefault();
          onDecide(true, write ? ["mcp:read", "mcp:write"] : ["mcp:read"]);
        }}
      >
        {me && (
          <div className="flex items-center gap-3 border-b border-line px-5 py-3 text-sm">
            <UserAvatar user={me} size={28} />
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">{me.displayName}</p>
              <p className="truncate text-xs text-muted">{me.email}</p>
            </div>
            <button type="button" onClick={signOut} className="text-[13px] font-medium text-accent hover:underline">
              Not you?
            </button>
          </div>
        )}

        <div role="group" aria-labelledby="consent-access" className="px-5 pt-4 pb-1">
          <p id="consent-access" className="mb-2 text-sm font-medium">
            Workflow will let {app.name}:
          </p>
          <div className="flex flex-col gap-2">
            <ScopeOption
              checked={!write}
              onSelect={() => setWrite(false)}
              icon={<BookOpen className="size-4" />}
              title="Read your issues"
              body="Search and read issues, comments, projects, boards, sprints and metrics you can see."
              selectable={offersWrite}
            />
            {offersWrite && (
              <ScopeOption
                checked={write}
                onSelect={() => setWrite(true)}
                icon={<PencilLine className="size-4" />}
                title="Read your issues, and create, edit and move them"
                body="Also file issues, change fields, comment, link issues and move them through workflows."
                selectable
              />
            )}
          </div>
        </div>

        <dl className="mx-5 mt-3 grid grid-cols-[6.5rem_1fr] gap-x-3 gap-y-2 rounded-md bg-surface-2 px-3 py-2.5 text-[13px]">
          <dt className="text-muted">App</dt>
          <dd className="min-w-0">
            {app.kind === "metadata" ? (
              <span className="flex items-center gap-1.5">
                <ShieldCheck className="size-3.5 shrink-0 text-success" aria-hidden />
                <span className="truncate">
                  Published by <span className="font-mono font-medium text-ink">{app.host}</span>
                </span>
              </span>
            ) : (
              <span>
                “{app.name}” <span className="text-muted">— the name the app gave itself</span>
              </span>
            )}
          </dd>
          <dt className="text-muted">Returns you to</dt>
          <dd className="min-w-0">
            <span className="flex items-center gap-1.5">
              {consent.nativeRedirect && <Laptop className="size-3.5 shrink-0 text-muted" aria-hidden />}
              <span className="truncate font-mono font-medium text-ink" title={consent.redirectUri}>
                {consent.redirectHost}
              </span>
              {consent.nativeRedirect && <span className="shrink-0 text-muted">(this computer)</span>}
            </span>
          </dd>
        </dl>

        <div className="px-5 pt-3">
          {consent.nativeRedirect ? (
            <Callout tone="warning">
              {app.name} receives access through an app on this computer. Only continue if you started connecting {app.name} here yourself just now.
            </Callout>
          ) : (
            app.kind === "dynamic" && (
              <Callout tone="info">
                Only continue if you started connecting {app.name} from <strong className="font-medium text-ink">{consent.redirectHost}</strong> yourself.
              </Callout>
            )
          )}
          {error != null && <p className="mt-3 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger">{errorMessage(error)}</p>}
        </div>

        <div className="mt-4 flex items-center justify-end gap-2 border-t border-line px-5 py-3">
          <Button onClick={() => onDecide(false)}>Deny</Button>
          <Button type="submit" variant="primary">
            Allow access
          </Button>
        </div>
      </form>
      <p className="mt-4 px-2 text-center text-xs text-muted">
        Changes {app.name} makes are recorded under your name. You can disconnect it at any time from your profile, under Connected apps.
      </p>
    </Frame>
  );
}

function ScopeOption({ checked, onSelect, icon, title, body, selectable }: { checked: boolean; onSelect: () => void; icon: ReactNode; title: string; body: string; selectable: boolean }) {
  const inner = (
    <>
      <span className={cn("mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md", checked ? "bg-accent-soft text-accent" : "bg-surface-2 text-muted")}>{icon}</span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="mt-0.5 block text-[13px] text-muted">{body}</span>
      </span>
    </>
  );
  if (!selectable) return <div className="flex items-start gap-3 rounded-md border border-line px-3 py-2.5">{inner}</div>;
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5 has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-accent",
        checked ? "border-accent bg-accent-soft/40" : "border-line hover:bg-surface-2",
      )}
    >
      <input type="radio" name="access" className="sr-only" checked={checked} onChange={onSelect} />
      {inner}
      <span aria-hidden className={cn("mt-1 flex size-4 shrink-0 items-center justify-center rounded-full border", checked ? "border-accent" : "border-line-strong")}>
        {checked && <span className="size-2 rounded-full bg-accent" />}
      </span>
    </label>
  );
}
