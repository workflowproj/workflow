import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Checkbox, Field, Input, Modal, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CodeBlock, SecretOnce } from "../common";

export type Webhook = Schemas["WebhookSubscription"];
type EventName = Schemas["WebhookSubscriptionInput"]["events"][number];

export const WEBHOOK_EVENTS: { id: Exclude<EventName, "*">; label: string }[] = [
  { id: "issue.created", label: "Issue created" },
  { id: "issue.updated", label: "Issue updated" },
  { id: "issue.transitioned", label: "Issue transitioned" },
  { id: "issue.deleted", label: "Issue deleted" },
  { id: "comment.created", label: "Comment added" },
  { id: "sprint.started", label: "Sprint started" },
  { id: "sprint.completed", label: "Sprint completed" },
  { id: "release.released", label: "Release shipped" },
  { id: "deployment.completed", label: "Deployment completed" },
];

/** WebhookDialog creates or edits a webhook; after creation it shows the signing secret once. */
export function WebhookDialog({ projectKey, webhook, onClose }: { projectKey?: string; webhook?: Webhook; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(webhook?.name ?? "");
  const [url, setUrl] = useState(webhook?.url ?? "https://");
  const [events, setEvents] = useState<EventName[]>((webhook?.events as EventName[]) ?? ["issue.created", "issue.transitioned"]);
  const [active, setActive] = useState(webhook?.active ?? true);
  const [created, setCreated] = useState<Schemas["WebhookSubscriptionCreated"] | null>(null);
  const all = events.includes("*");

  const save = useMutation({
    mutationFn: () => {
      const body: Schemas["WebhookSubscriptionInput"] = { name: name.trim(), url: url.trim(), events, active, ...(projectKey ? { projectKey } : {}) };
      return webhook ? unwrap(api.PUT("/webhooks/{webhookId}", { params: { path: { webhookId: webhook.id } }, body })) : unwrap(api.POST("/webhooks", { body }));
    },
    onSuccess: (w) => {
      qc.invalidateQueries({ queryKey: ["webhooks", projectKey ?? ""] });
      if (!webhook && "secret" in w) setCreated(w as Schemas["WebhookSubscriptionCreated"]);
      else {
        toast.success(`Saved ${w.name}`);
        onClose();
      }
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};

  if (created) {
    return (
      <Modal
        open
        onClose={onClose}
        title={`${created.name} is ready`}
        size="lg"
        footer={
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        }
      >
        <div className="flex flex-col gap-4">
          <SecretOnce label="Signing secret" value={created.secret} />
          <SignatureHelp secretHint />
        </div>
      </Modal>
    );
  }

  const toggle = (e: EventName, on: boolean) => setEvents(on ? [...events.filter((x) => x !== "*"), e] : events.filter((x) => x !== e));
  return (
    <Modal
      open
      onClose={onClose}
      title={webhook ? `Edit ${webhook.name}` : "New webhook"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="webhook-form" loading={save.isPending} disabled={!name.trim() || !url.trim() || events.length === 0}>
            {webhook ? "Save" : "Create webhook"}
          </Button>
        </>
      }
    >
      <form
        id="webhook-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="grid gap-4 sm:grid-cols-[14rem_1fr]">
          <Field label="Name" required error={fe.name}>
            {(id) => <Input id={id} autoFocus value={name} maxLength={100} onChange={(e) => setName(e.target.value)} placeholder="Slack bridge" />}
          </Field>
          <Field label="Payload URL" required error={fe.url} hint="Receives a JSON POST for each event.">
            {(id) => <Input id={id} type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://hooks.example.com/wfm" invalid={!!fe.url} />}
          </Field>
        </div>
        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium">Events</legend>
          <Checkbox label={<span className="font-medium">All events, including ones added later</span>} checked={all} onChange={(e) => setEvents(e.target.checked ? ["*"] : ["issue.created"])} />
          <div className="mt-2 grid gap-1.5 rounded-md border border-line p-3 sm:grid-cols-2">
            {WEBHOOK_EVENTS.map((e) => (
              <Checkbox
                key={e.id}
                label={
                  <span>
                    {e.label} <code className="ml-1 font-mono text-[11px] text-faint">{e.id}</code>
                  </span>
                }
                checked={all || events.includes(e.id)}
                disabled={all}
                onChange={(ev) => toggle(e.id, ev.target.checked)}
              />
            ))}
          </div>
          {fe.events && <p className="mt-1 text-xs text-danger">{fe.events}</p>}
        </fieldset>
        <Checkbox label="Active (deliver events)" checked={active} onChange={(e) => setActive(e.target.checked)} />
        {!webhook && <p className="text-xs text-muted">A signing secret is generated and shown once after creation.</p>}
        {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}

export function SignatureHelp({ secretHint }: { secretHint?: boolean }) {
  return (
    <div className="flex flex-col gap-2 text-[13px]">
      <p className="text-muted">
        Every delivery is signed: the <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">X-WFM-Signature-256</code> header is{" "}
        <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">sha256=</code> followed by the hex HMAC-SHA256 of the raw request body
        {secretHint ? " with this secret" : " with the webhook's secret"}. Compare it in constant time before trusting the payload.{" "}
        <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">X-WFM-Event</code> names the event and{" "}
        <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">X-WFM-Delivery</code> identifies the delivery (retries reuse it).
      </p>
      <CodeBlock>{`// Node.js
const expected = "sha256=" + crypto.createHmac("sha256", secret).update(rawBody).digest("hex");
const ok = crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(req.headers["x-wfm-signature-256"]));`}</CodeBlock>
    </div>
  );
}
