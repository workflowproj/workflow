import { useMutation, useQuery } from "@tanstack/react-query";
import { Send } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { useMe } from "@/api/queries";
import { Badge, Button, PageHeader, PageSpinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, CopyField, SettingsSection } from "@/features/settings/common";

const tlsLabel = { starttls: "STARTTLS", tls: "TLS", none: "unencrypted" } as const;

export function EmailAdmin() {
  const toast = useToast();
  const { data: me } = useMe();
  const q = useQuery({ queryKey: ["admin-email"], queryFn: () => unwrap(api.GET("/admin/email")) });
  const test = useMutation({
    mutationFn: () => unwrap(api.POST("/admin/email/test")),
    onSuccess: (r) => (r.transport === "smtp" ? toast.success(`Test email sent to ${r.to}`) : toast.success(`Test email kept in memory for ${r.to} (no SMTP server)`)),
    onError: toast.error,
  });
  if (q.isLoading) return <PageSpinner />;
  if (q.error || !q.data) return <ErrorBox error={q.error ?? new Error("Not available")} />;
  const s = q.data;
  const smtp = s.transport === "smtp";
  const minutes = Math.round(s.batchWindowSeconds / 60);
  return (
    <div className="flex flex-col gap-5">
      <PageHeader title="Email" subtitle="Outgoing email for notifications and daily digests. People choose what they get in their profile." />
      <SettingsSection
        title="Outgoing email"
        description="Configured with environment variables on the server."
        actions={
          <Button size="sm" variant="primary" icon={<Send className="size-3.5" />} loading={test.isPending} onClick={() => test.mutate()}>
            Send test email
          </Button>
        }
      >
        <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-[12rem_1fr]">
          <dt className="text-muted">Delivery</dt>
          <dd className="flex flex-wrap items-center gap-2">
            {smtp ? (
              <>
                <Badge tone="success">SMTP</Badge>
                <code className="font-mono text-[13px]">
                  {s.host}:{s.port}
                </code>
                {s.tls && <span className="text-[13px] text-muted">{tlsLabel[s.tls]}</span>}
              </>
            ) : (
              <>
                <Badge tone="warning">In memory only</Badge>
                <span className="text-[13px] text-muted">No SMTP server: emails are logged and kept in memory, not delivered.</span>
              </>
            )}
          </dd>
          <dt className="text-muted">From</dt>
          <dd>
            <code className="font-mono text-[13px]">{s.from}</code>
          </dd>
          {smtp && (
            <>
              <dt className="text-muted">SMTP user</dt>
              <dd className="text-[13px]">{s.username ? <code className="font-mono">{s.username}</code> : <span className="text-muted">None (no authentication)</span>}</dd>
            </>
          )}
          <dt className="text-muted">Links in emails</dt>
          <dd>
            <code className="font-mono text-[13px]">{s.publicUrl}</code>
            <span className="ml-2 text-[13px] text-muted">WFM_PUBLIC_URL</span>
          </dd>
          <dt className="text-muted">One-click unsubscribe</dt>
          <dd className="flex min-w-0 flex-col gap-1">
            <CopyField value={s.unsubscribeUrl} />
            <span className="text-[13px] text-muted">On WFM_HOOKS_PUBLIC_URL, so mail providers can reach it (RFC 8058). Sign outgoing mail with DKIM for providers to honour it.</span>
          </dd>
          <dt className="text-muted">Immediate emails</dt>
          <dd className="text-[13px]">
            Wait {minutes >= 1 ? `${minutes} minute${minutes === 1 ? "" : "s"}` : `${s.batchWindowSeconds} seconds`} so several changes to one issue arrive as one email{" "}
            <span className="text-muted">(WFM_EMAIL_BATCH_WINDOW)</span>
          </dd>
        </dl>
        {!smtp && (
          <div className="px-4 pb-4">
            <Callout title="Deliver email through an SMTP server">
              Set <code>WFM_SMTP_HOST</code>, <code>WFM_SMTP_PORT</code>, <code>WFM_SMTP_USERNAME</code>, <code>WFM_SMTP_PASSWORD</code> and <code>WFM_SMTP_FROM</code> (and <code>WFM_SMTP_TLS</code> if the
              server does not use STARTTLS), then restart. In development, the kept emails are listed at <code>/api/v1/dev/mailbox</code>.
            </Callout>
          </div>
        )}
        <p className="border-t border-line px-4 py-3 text-[13px] text-muted">The test email goes to you{me ? ` (${me.email})` : ""} right away, so connection or sign-in problems show here instead of in the logs.</p>
      </SettingsSection>
      <SettingsSection title="Notification emails" description="What people get unless they change it in Profile → Email notifications.">
        <ul className="flex flex-col gap-1.5 p-4 text-sm">
          <li>
            <span className="font-medium">Assigned to me</span> <span className="text-muted">— immediately</span>
          </li>
          <li>
            <span className="font-medium">Comments, status changes and automation</span> <span className="text-muted">— in a daily digest at 09:00 (UTC until people pick their time zone)</span>
          </li>
          <li className="text-[13px] text-muted">Nobody is emailed about their own changes, about projects they can no longer see, or once deactivated. Service accounts get no email.</li>
        </ul>
      </SettingsSection>
    </div>
  );
}
