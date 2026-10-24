import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, FileCode2, Lock, Plus } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useMe } from "@/api/queries";
import { Badge, Button, PageHeader, PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { SettingsSection } from "@/features/settings/common";
import { EmailNotificationsSection } from "@/features/settings/email/EmailNotificationsSection";
import { ConnectSnippets } from "@/features/settings/tokens/ConnectSnippets";
import { CreateTokenDialog, TokenTable } from "@/features/settings/tokens/Tokens";
import { ConnectedAppsSection } from "@/features/settings/connected-apps/ConnectedAppsSection";
import { SourceBadge, sourceLabel } from "@/features/admin/badges";

const managed = new Set(["scim", "google"]);

export function ProfilePage() {
  const { data: me } = useMe();
  const qc = useQueryClient();
  const toast = useToast();
  const [creating, setCreating] = useState(false);
  const tokens = useQuery({ queryKey: ["my-tokens"], queryFn: () => unwrap(api.GET("/me/tokens")) });
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/me/tokens/{tokenId}", { params: { path: { tokenId: id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["my-tokens"] });
      toast.success("Token revoked");
    },
    onError: toast.error,
  });
  if (!me) return <PageSpinner />;
  const source = me.source;
  const server = window.location.origin;

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6 p-6">
      <PageHeader title="Profile" subtitle="Your account, the tokens that act as you, and how to connect AI assistants and scripts." />
      <SettingsSection title="Account">
        <div className="flex flex-wrap items-start gap-5 p-5">
          <UserAvatar user={me} size={64} />
          <dl className="grid min-w-0 flex-1 grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted">Name</dt>
            <dd className="font-medium">{me.displayName}</dd>
            <dt className="text-muted">Email</dt>
            <dd>{me.email}</dd>
            <dt className="text-muted">Organisation role</dt>
            <dd>
              <Badge tone={me.orgRole === "admin" ? "accent" : "neutral"}>{me.orgRole}</Badge>
            </dd>
            {source && (
              <>
                <dt className="text-muted">Provisioned by</dt>
                <dd>
                  <SourceBadge source={source} />
                </dd>
              </>
            )}
          </dl>
        </div>
        <div className="flex items-start gap-2 border-t border-line px-5 py-3 text-[13px] text-muted">
          <Lock className="mt-0.5 size-3.5 shrink-0" />
          {source && managed.has(source)
            ? `Your profile is managed by ${sourceLabel(source)}: name, email and status come from your identity provider and are read-only here.`
            : "Your name, email and picture come from your sign-in provider and update when you sign in. Ask an administrator to change your role."}
        </div>
      </SettingsSection>

      <EmailNotificationsSection />

      <SettingsSection
        title="Personal access tokens"
        description="For AI assistants, scripts, CI and the wfm CLI. A token can do what you can do, limited by its scopes: read tokens only look."
        actions={
          <Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => setCreating(true)}>
            New token
          </Button>
        }
      >
        {tokens.error && <ErrorBox error={tokens.error} className="m-4" />}
        {tokens.isLoading ? <PageSpinner /> : <TokenTable tokens={tokens.data ?? []} onRevoke={(t) => revoke.mutate(t.id)} revoking={revoke.isPending ? revoke.variables : undefined} />}
      </SettingsSection>

      <ConnectedAppsSection />

      <SettingsSection
        title="Connect AI assistants and tools"
        description="Claude and other AI assistants can search, read, create and update issues, move them through workflows and read sprint and flow metrics over MCP, as you. Add Workflow by its address and approve access in your browser."
      >
        <div className="flex flex-col gap-4 p-4">
          <ConnectSnippets server={server} />
          <div className="grid gap-3 text-[13px] text-muted sm:grid-cols-2">
            <p className="flex gap-2">
              <Bot className="mt-0.5 size-4 shrink-0 text-faint" />
              <span>
                MCP endpoint <code className="font-mono text-ink">{server}/mcp</code>. Tools: search issues (JQL), get an issue, project, board or sprint report, metrics, create, update, transition, comment and link
                issues. Changes appear in the issue history under your name.
              </span>
            </p>
            <p className="flex gap-2">
              <FileCode2 className="mt-0.5 size-4 shrink-0 text-faint" />
              <span>
                REST API at <code className="font-mono text-ink">{server}/api/v1</code>, described by{" "}
                <a className="font-medium text-accent hover:underline" href="/api/v1/openapi.yaml" target="_blank" rel="noreferrer">
                  openapi.yaml
                </a>{" "}
                (or{" "}
                <a className="font-medium text-accent hover:underline" href="/api/v1/openapi.json" target="_blank" rel="noreferrer">
                  JSON
                </a>
                ).
              </span>
            </p>
          </div>
        </div>
      </SettingsSection>

      <CreateTokenDialog
        open={creating}
        onClose={() => setCreating(false)}
        allowAdmin={me.orgRole === "admin"}
        create={async (body) => {
          const t = await unwrap(api.POST("/me/tokens", { body }));
          qc.invalidateQueries({ queryKey: ["my-tokens"] });
          return t;
        }}
        after={(token) => <ConnectSnippets server={server} token={token} />}
      />
    </div>
  );
}
