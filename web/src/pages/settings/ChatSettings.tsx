import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MessagesSquare, Pencil, Plus, ScrollText, Send, Terminal, Trash2 } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useMe } from "@/api/queries";
import { Badge, Button, EmptyState, IconButton, PageHeader, PageSpinner, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ChannelDialog } from "@/features/settings/chat/ChannelDialog";
import { DeliveriesDrawer, deliveryTone } from "@/features/settings/chat/DeliveriesDrawer";
import { chatChannelsQuery, chatDeliveriesQuery, eventLabel, providerOf, type ChatChannel } from "@/features/settings/chat/meta";
import { CodeBlock, CopyField, SettingsSection, Switch, When } from "@/features/settings/common";

const route = getRouteApi("/app/projects/$projectKey/settings/chat");

export function ChatSettings() {
  const { projectKey } = route.useParams();
  const channels = useQuery(chatChannelsQuery(projectKey));
  const [dialog, setDialog] = useState<ChatChannel | "new" | null>(null);
  const [log, setLog] = useState<ChatChannel | null>(null);
  const list = channels.data ?? [];

  return (
    <div className="flex max-w-5xl flex-col gap-5">
      <PageHeader
        title="Chat"
        subtitle="Post what happens in this project to Slack or Mattermost channels. Automation rules can post to these channels too."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setDialog("new")}>
            Add channel
          </Button>
        }
      />
      {channels.error && <ErrorBox error={channels.error} />}
      {channels.isLoading ? (
        <PageSpinner />
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<MessagesSquare className="size-8" />}
            title="No chat channels yet"
            body="Add a channel's incoming webhook to post new issues, status changes, comments, sprints, releases, merged pull requests and failed pipelines."
            action={<Button onClick={() => setDialog("new")}>Add channel</Button>}
          />
        </div>
      ) : (
        <ul className="flex flex-col gap-3">
          {list.map((c) => (
            <ChannelCard key={c.id} channel={c} projectKey={projectKey} onEdit={() => setDialog(c)} onLog={() => setLog(c)} />
          ))}
        </ul>
      )}
      {list.length > 0 && (
        <p className="text-[13px] text-muted">
          Want a custom message, e.g. when an epic finishes?{" "}
          <Link to="/projects/$projectKey/settings/automation" params={{ projectKey }} className="text-accent hover:underline">
            Add an automation rule
          </Link>{" "}
          with the “Post to a chat channel” action.
        </p>
      )}
      <SlashCommandHelp />
      {dialog && <ChannelDialog projectKey={projectKey} channel={dialog === "new" ? undefined : dialog} onClose={() => setDialog(null)} />}
      {log && <DeliveriesDrawer channel={log} onClose={() => setLog(null)} />}
    </div>
  );
}

function ChannelCard({ channel: c, projectKey, onEdit, onLog }: { channel: ChatChannel; projectKey: string; onEdit: () => void; onLog: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const p = providerOf(c.provider);
  const last = useQuery(chatDeliveriesQuery(c.id, 1));
  const lastDelivery = last.data?.[0];
  const invalidate = () => qc.invalidateQueries({ queryKey: ["chat-channels", projectKey] });
  const setActive = useMutation({
    mutationFn: (active: boolean) => unwrap(api.PATCH("/chat-channels/{channelId}", { params: { path: { channelId: c.id } }, body: { active } })),
    onSuccess: (x) => {
      invalidate();
      toast.success(`${x.name} ${x.active ? "resumed" : "paused"}`);
    },
    onError: toast.error,
  });
  const test = useMutation({
    mutationFn: () => unwrap(api.POST("/chat-channels/{channelId}/test", { params: { path: { channelId: c.id } } })),
    onSuccess: () => {
      toast.success(`Test message queued for ${c.name}`);
      setTimeout(() => qc.invalidateQueries({ queryKey: ["chat-deliveries", c.id] }), 1500);
    },
    onError: toast.error,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/chat-channels/{channelId}", { params: { path: { channelId: c.id } } })),
    onSuccess: () => {
      invalidate();
      qc.invalidateQueries({ queryKey: ["automation-rules", projectKey] });
      toast.success(`Removed ${c.name}`);
    },
    onError: toast.error,
  });
  const everything = c.events.includes("*");
  return (
    <li className="rounded-lg border border-line bg-surface p-4">
      <div className="flex flex-wrap items-start gap-3">
        <span className={`flex size-9 shrink-0 items-center justify-center rounded-md ${c.active ? "bg-accent-soft text-accent" : "bg-surface-2 text-faint"}`}>
          <p.icon className="size-5" aria-hidden />
        </span>
        <div className="min-w-0 flex-1 basis-60">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold break-all">{c.name}</h3>
            <Badge>{p.label}</Badge>
            {!c.active && <Badge tone="warning">Paused</Badge>}
          </div>
          <p className="mt-0.5 font-mono text-xs break-all text-muted" title="The webhook URL is stored encrypted; only its host and last characters are shown">
            {c.urlHint}
          </p>
          <div className="mt-2 flex flex-wrap gap-1">
            {everything ? (
              <Badge tone="accent" className="normal-case">
                Everything
              </Badge>
            ) : (
              c.events.map((e) => (
                <Badge key={e} className="normal-case">
                  {eventLabel(e)}
                </Badge>
              ))
            )}
          </div>
          {c.jql && (
            <p className="mt-1.5 text-[13px] text-muted">
              Only issues matching <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">{c.jql}</code>
            </p>
          )}
          <p className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-muted">
            Last message:{" "}
            {lastDelivery ? (
              <>
                <Badge tone={deliveryTone[lastDelivery.status]}>{lastDelivery.status}</Badge>
                {eventLabel(lastDelivery.event)}
                {lastDelivery.issueKey && ` · ${lastDelivery.issueKey}`} · <When iso={lastDelivery.deliveredAt ?? lastDelivery.createdAt} />
                {lastDelivery.status === "failed" && lastDelivery.lastError && <span className="w-full break-words text-danger">{lastDelivery.lastError}</span>}
              </>
            ) : (
              <span className="text-faint">none yet</span>
            )}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-1">
          <Switch checked={c.active} onChange={(v) => setActive.mutate(v)} disabled={setActive.isPending} label={`${c.active ? "Pause" : "Resume"} ${c.name}`} hideLabel />
          <Button
            size="sm"
            icon={<Send className="size-3.5" />}
            loading={test.isPending}
            disabled={!c.active}
            onClick={() => test.mutate()}
            title={c.active ? "Post a test message" : "Resume the channel to test it"}
          >
            Send test
          </Button>
          <IconButton label={`Delivery log of ${c.name}`} size="sm" onClick={onLog}>
            <ScrollText className="size-4" />
          </IconButton>
          <IconButton label={`Edit ${c.name}`} size="sm" onClick={onEdit}>
            <Pencil className="size-4" />
          </IconButton>
          <IconButton
            label={`Remove ${c.name}`}
            size="sm"
            onClick={async () => {
              if (
                await confirm({
                  title: `Remove ${c.name}?`,
                  body: "Messages to it stop, and automation rules that post to it are paused until you pick another channel.",
                  confirmLabel: "Remove channel",
                  danger: true,
                })
              )
                remove.mutate();
            }}
          >
            <Trash2 className="size-4" />
          </IconButton>
        </div>
      </div>
    </li>
  );
}

/** SlashCommandHelp explains the /wfm command; its request URLs live under the hooks base URL. */
function SlashCommandHelp() {
  const { data: me } = useMe();
  // The hooks base URL is what SCIM and SCM webhook URLs use; admins can read it from provisioning.
  const prov = useQuery({ queryKey: ["provisioning"], queryFn: () => unwrap(api.GET("/admin/provisioning")), enabled: me?.orgRole === "admin", staleTime: 60_000 });
  const hooks = prov.data?.scim.baseUrl.replace(/\/scim\/v2\/?$/, "") ?? window.location.origin;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Terminal className="size-4 text-muted" /> The /wfm command
        </span>
      }
      description="Look up issues and run searches from Slack or Mattermost. Answers are only visible to whoever asked."
    >
      <div className="grid gap-4 p-4 md:grid-cols-2">
        <div className="flex flex-col gap-2 text-[13px]">
          <CodeBlock>{`/wfm PLAT-12\n/wfm search type = Bug AND statusCategory != Done\n/wfm help`}</CodeBlock>
          <p className="text-muted">Read-only, and only open projects (or those in WFM_CHAT_COMMAND_PROJECTS). An administrator sets it up once for the whole organisation; see docs/chat.md.</p>
        </div>
        <div className="flex flex-col gap-2 text-[13px]">
          <div>
            <p className="mb-1 font-medium">Slack request URL</p>
            <CopyField value={`${hooks}/hooks/chat/slack`} />
            <p className="mt-1 text-xs text-muted">
              Slash Commands → Create New Command <code className="rounded bg-surface-2 px-1 font-mono">/wfm</code>; put the app's Signing Secret in{" "}
              <code className="rounded bg-surface-2 px-1 font-mono">WFM_SLACK_SIGNING_SECRET</code>.
            </p>
          </div>
          <div>
            <p className="mb-1 font-medium">Mattermost request URL</p>
            <CopyField value={`${hooks}/hooks/chat/mattermost`} />
            <p className="mt-1 text-xs text-muted">
              Integrations → Slash Commands → Add, trigger word <code className="rounded bg-surface-2 px-1 font-mono">wfm</code>, POST; put its token in{" "}
              <code className="rounded bg-surface-2 px-1 font-mono">WFM_MATTERMOST_COMMAND_TOKENS</code>.
            </p>
          </div>
          {!prov.data && <p className="text-xs text-faint">Shown with this site's address; if hooks are served elsewhere (WFM_HOOKS_PUBLIC_URL), use that host.</p>}
        </div>
      </div>
    </SettingsSection>
  );
}
