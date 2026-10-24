import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Checkbox, Field, Input, Modal, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { JqlEditor, type JqlError } from "@/features/search/JqlEditor";
import { cn } from "@/lib/cn";
import { Callout } from "../common";
import { EVENT_GROUPS, PROVIDERS, providerOf, type ChatChannel, type ChatEvent, type ChatProvider } from "./meta";

/** ChannelDialog adds a Slack / Mattermost channel or edits one (the webhook URL is write-only). */
export function ChannelDialog({ projectKey, channel, onClose }: { projectKey: string; channel?: ChatChannel; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const editing = !!channel;
  const [provider, setProvider] = useState<ChatProvider>(channel?.provider ?? "slack");
  const [name, setName] = useState(channel?.name ?? "");
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<ChatEvent[]>(channel?.events ?? ["issue.created", "issue.transitioned", "pr.merged", "release.released"]);
  const [jql, setJql] = useState(channel?.jql ?? "");
  const [active, setActive] = useState(channel?.active ?? true);
  const everything = events.includes("*");
  const p = providerOf(provider);

  const save = useMutation({
    mutationFn: () => {
      if (channel) {
        const body: Schemas["ChatChannelPatch"] = { name: name.trim(), events, jql: jql.trim(), active };
        if (url.trim()) body.webhookUrl = url.trim();
        return unwrap(api.PATCH("/chat-channels/{channelId}", { params: { path: { channelId: channel.id } }, body }));
      }
      return unwrap(
        api.POST("/projects/{projectKey}/chat-channels", { params: { path: { projectKey } }, body: { provider, name: name.trim(), webhookUrl: url.trim(), events, jql: jql.trim(), active } }),
      );
    },
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["chat-channels", projectKey] });
      toast.success(editing ? `Saved ${c.name}` : `Added ${c.name}. Use “Send test” to check it.`);
      onClose();
    },
  });
  const err = save.error instanceof ApiError ? save.error : null;
  const fe = err ? err.fieldErrors() : {};
  // JQL problems come back as a 400 with a character position rather than a field error.
  const jqlError: JqlError | null =
    err && !Object.keys(fe).length && err.status === 400 && (err.problem.position !== undefined || err.code?.startsWith("jql"))
      ? { message: err.problem.detail ?? err.message, index: err.problem.position }
      : null;
  const toggle = (e: ChatEvent, on: boolean) => setEvents(on ? [...events.filter((x) => x !== "*"), e] : events.filter((x) => x !== e));

  return (
    <Modal
      open
      onClose={onClose}
      title={editing ? `Edit ${channel.name}` : "Add a chat channel"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="chat-form" loading={save.isPending} disabled={!name.trim() || (!editing && !url.trim()) || events.length === 0}>
            {editing ? "Save" : "Add channel"}
          </Button>
        </>
      }
    >
      <form
        id="chat-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div role="radiogroup" aria-label="Chat app" className="grid grid-cols-2 gap-2">
          {PROVIDERS.map((x) => (
            <button
              key={x.id}
              type="button"
              role="radio"
              aria-checked={provider === x.id}
              disabled={editing && provider !== x.id}
              onClick={() => setProvider(x.id)}
              className={cn(
                "flex items-center gap-2.5 rounded-md border p-3 text-left disabled:opacity-40",
                provider === x.id ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong",
              )}
            >
              <x.icon className={cn("size-5", provider === x.id ? "text-accent" : "text-muted")} />
              <span className="text-sm font-medium">{x.label}</span>
            </button>
          ))}
        </div>
        {!editing && (
          <div className="rounded-md border border-line bg-surface-2/60 px-3 py-2.5 text-[13px]">
            <p className="mb-1 font-medium">Get an incoming webhook URL from {p.label}</p>
            <ol className="list-decimal space-y-0.5 pl-5 text-muted">
              {p.steps.map((s) => (
                <li key={s}>{s}</li>
              ))}
            </ol>
          </div>
        )}
        <div className="grid gap-4 sm:grid-cols-[13rem_1fr]">
          <Field label="Name" required error={fe.name} hint="How people recognise it">
            {(id) => <Input id={id} autoFocus value={name} maxLength={80} onChange={(e) => setName(e.target.value)} placeholder="#team-payments" invalid={!!fe.name} />}
          </Field>
          <Field label="Webhook URL" required={!editing} error={fe.webhookUrl} hint={editing ? `Currently ${channel.urlHint}. Leave empty to keep it.` : "Stored encrypted and never shown again."}>
            {(id) => (
              <Input
                id={id}
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder={editing ? "Unchanged" : p.urlExample}
                invalid={!!fe.webhookUrl}
                className="font-mono text-[13px]"
              />
            )}
          </Field>
        </div>
        {provider === "mattermost" && (
          <Callout tone="info" title="Self-hosted Mattermost on a private network?">
            Outbound calls to private addresses are blocked. An administrator can allow your Mattermost host with <code>WFM_OUTBOUND_ALLOWED_HOSTS=mattermost.corp.example</code>.
          </Callout>
        )}
        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium">Post when</legend>
          <Checkbox
            label={<span className="font-medium">Everything, including events added later</span>}
            checked={everything}
            onChange={(e) => setEvents(e.target.checked ? ["*"] : ["issue.created"])}
          />
          <div className="mt-2 grid gap-3 rounded-md border border-line p-3 sm:grid-cols-3">
            {EVENT_GROUPS.map((g) => (
              <div key={g.title}>
                <p className="mb-1.5 text-[11px] font-semibold tracking-wide text-faint uppercase">{g.title}</p>
                <div className="flex flex-col gap-1.5">
                  {g.events.map((e) => (
                    <Checkbox key={e.id} label={e.label} checked={everything || events.includes(e.id)} disabled={everything} onChange={(ev) => toggle(e.id, ev.target.checked)} />
                  ))}
                </div>
              </div>
            ))}
          </div>
          {fe.events && <p className="mt-1 text-xs text-danger">{fe.events}</p>}
        </fieldset>
        <div>
          <p className="mb-1 text-[13px] font-medium">Only issues matching (optional)</p>
          <JqlEditor value={jql} onChange={setJql} onRun={() => {}} error={jqlError} projectKey={projectKey} placeholder="type = Bug AND priority in (Highest, High)" />
          <p className="mt-1 text-xs text-muted">Filters issue events (not sprints and releases), e.g. for an on-call channel. currentUser() matches nobody here.</p>
        </div>
        <Checkbox label="Active (post messages)" checked={active} onChange={(e) => setActive(e.target.checked)} />
        {err && Object.keys(fe).length === 0 && !jqlError && <ErrorBox error={err} />}
      </form>
    </Modal>
  );
}
