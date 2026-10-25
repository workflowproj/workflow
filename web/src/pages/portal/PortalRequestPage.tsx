import { useRef, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleX, Download, MessageSquare, Paperclip, Send, ShieldCheck, Users } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { Badge, Button, EmptyState, PageSpinner, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Markdown } from "@/components/Markdown";
import { StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { formatBytes, formatDate, formatDateTime, relativeTime } from "@/lib/format";
import { RequestTypeIcon } from "@/features/servicedesk/bits";
import { portalRequestQuery, type Approval, type PortalRequestDetail } from "@/features/servicedesk/api";
import { Crumbs } from "./parts";

const route = getRouteApi("/portal/requests/$issueKey");

/** PortalRequestPage is a request as its reporter, the people it is shared with and its approvers see it. */
export function PortalRequestPage() {
  const { issueKey } = route.useParams();
  const { created } = route.useSearch();
  const { data: r, isLoading, error } = useQuery(portalRequestQuery(issueKey));
  if (isLoading) return <PageSpinner label={`Loading ${issueKey}`} />;
  if (!r) {
    const missing = error instanceof ApiError && error.status === 404;
    return missing ? (
      <div className="rounded-lg border border-line bg-surface">
        <EmptyState title={`${issueKey} is not available`} body="The request does not exist, or it is not shared with you." />
      </div>
    ) : (
      <ErrorBox error={error} />
    );
  }
  return (
    <div>
      <Crumbs items={[{ label: "Help centre", to: "/portal" }, { label: "My requests", to: "/portal/requests" }, { label: r.key }]} />
      {created && (
        <div role="status" className="mb-5 flex items-start gap-2.5 rounded-lg border border-success/30 bg-success-soft px-4 py-3 text-sm">
          <CheckCircle2 aria-hidden className="mt-0.5 size-4 shrink-0 text-success" />
          <div>
            <p className="font-medium text-ink">Your request {r.key} was sent</p>
            <p className="text-muted">We emailed you a confirmation. Follow it here, and reply if the team asks you something.</p>
          </div>
        </div>
      )}
      <header className="flex items-start gap-3">
        <RequestTypeIcon icon={r.requestType.icon} size="lg" />
        <div className="min-w-0 flex-1">
          <h1 className="text-xl font-semibold tracking-tight sm:text-2xl">{r.summary}</h1>
          <p className="mt-1 flex flex-col gap-0.5 text-[13px] text-muted sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-2">
            <span className="font-mono">{r.key}</span>
            <span aria-hidden className="hidden sm:inline">
              ·
            </span>
            <span>
              {r.requestType.name} · {r.desk.name}
            </span>
            <span aria-hidden className="hidden sm:inline">
              ·
            </span>
            <span>
              raised by {r.reporter.displayName} on {formatDate(r.createdAt)}
            </span>
          </p>
        </div>
        <div className="shrink-0 text-right">
          <span className="sr-only">Status: </span>
          <StatusBadge status={r.status} />
          {r.resolution && <p className="mt-1 text-xs text-muted">{r.resolution}</p>}
        </div>
      </header>

      <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="flex min-w-0 flex-col gap-6">
          {r.approvals.map((a) => (
            <ApprovalCard key={a.id} request={r} approval={a} />
          ))}
          <Conversation request={r} />
        </div>
        <aside className="flex min-w-0 flex-col gap-4">
          <Details request={r} />
          <Attachments request={r} />
          {r.participants.length > 0 && (
            <section aria-labelledby="shared" className="rounded-lg border border-line bg-surface p-4">
              <h2 id="shared" className="mb-2 flex items-center gap-1.5 text-sm font-semibold">
                <Users aria-hidden className="size-4 text-muted" /> Shared with
              </h2>
              <ul className="flex flex-col gap-1.5">
                {r.participants.map((p) => (
                  <li key={p.id} className="flex items-center gap-2 text-sm">
                    <UserAvatar user={p} size={22} />
                    {p.displayName}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </aside>
      </div>
    </div>
  );
}

function Details({ request: r }: { request: PortalRequestDetail }) {
  return (
    <section aria-labelledby="details" className="rounded-lg border border-line bg-surface p-4">
      <h2 id="details" className="mb-3 text-sm font-semibold">
        Details
      </h2>
      <dl className="flex flex-col gap-3 text-sm">
        {r.fields.map((f, i) => (
          <div key={i}>
            <dt className="text-[13px] text-muted">{f.label}</dt>
            <dd className="mt-0.5 break-words">{f.type === "textarea" ? <Markdown issueLinks={false}>{f.value}</Markdown> : f.value}</dd>
          </div>
        ))}
        <div>
          <dt className="text-[13px] text-muted">Last update</dt>
          <dd className="mt-0.5">
            <time dateTime={r.updatedAt} title={formatDateTime(r.updatedAt)}>
              {relativeTime(r.updatedAt)}
            </time>
          </dd>
        </div>
      </dl>
    </section>
  );
}

function Conversation({ request: r }: { request: PortalRequestDetail }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [body, setBody] = useState("");
  const reply = useMutation({
    mutationFn: (text: string) => unwrap(api.POST("/portal/requests/{issueKey}/comments", { params: { path: { issueKey: r.key } }, body: { body: text } })),
    onSuccess: () => {
      setBody("");
      qc.invalidateQueries({ queryKey: ["portal"] });
      toast.success("Reply sent");
    },
  });
  return (
    <section aria-labelledby="conversation">
      <h2 id="conversation" className="mb-3 text-base font-semibold">
        Conversation
      </h2>
      {r.comments.length === 0 ? (
        <p className="mb-4 flex items-center gap-2 text-sm text-muted">
          <MessageSquare aria-hidden className="size-4" /> No replies yet. The team will reply here and by email.
        </p>
      ) : (
        <ol className="mb-5 flex flex-col gap-4">
          {r.comments.map((c) => (
            <li key={c.id} className="flex gap-3">
              <UserAvatar user={c.author} size={30} className="mt-0.5" />
              <div className="min-w-0 flex-1 rounded-lg border border-line bg-surface px-4 py-3">
                <p className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                  <span className="font-medium">{c.author.displayName}</span>
                  {c.fromSupport && <Badge tone="accent">Support</Badge>}
                  <time dateTime={c.createdAt} title={formatDateTime(c.createdAt)} className="text-xs text-muted">
                    {relativeTime(c.createdAt)}
                  </time>
                </p>
                <Markdown className="mt-1.5" issueLinks={false}>
                  {c.body}
                </Markdown>
              </div>
            </li>
          ))}
        </ol>
      )}
      {r.canComment && (
        <form
          className="rounded-lg border border-line bg-surface p-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (body.trim()) reply.mutate(body.trim());
          }}
        >
          <label htmlFor="portal-reply" className="text-[13px] font-medium">
            Reply to the team
          </label>
          <Textarea id="portal-reply" rows={4} className="mt-1.5" value={body} onChange={(e) => setBody(e.target.value)} placeholder="Add more details or answer a question…" />
          {reply.error ? <ErrorBox error={reply.error} className="mt-2" /> : null}
          <div className="mt-2 flex justify-end">
            <Button type="submit" variant="primary" icon={<Send className="size-4" />} loading={reply.isPending} disabled={!body.trim()}>
              Send reply
            </Button>
          </div>
        </form>
      )}
    </section>
  );
}

function Attachments({ request: r }: { request: PortalRequestDetail }) {
  const qc = useQueryClient();
  const toast = useToast();
  const input = useRef<HTMLInputElement>(null);
  const upload = useMutation({
    mutationFn: async (file: File) => {
      const form = new FormData();
      form.append("file", file);
      const res = await fetch(`/api/v1/portal/requests/${encodeURIComponent(r.key)}/attachments`, {
        method: "POST",
        body: form,
        credentials: "same-origin",
        headers: { "X-Requested-With": "XMLHttpRequest" },
      });
      if (!res.ok) throw new ApiError({ title: res.statusText, status: res.status, ...(await res.json().catch(() => ({}))) });
      return res.json();
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["portal", "request", r.key] });
      toast.success("File attached");
    },
    onError: toast.error,
  });
  if (!r.canComment && r.attachments.length === 0) return null;
  return (
    <section aria-labelledby="files" className="rounded-lg border border-line bg-surface p-4">
      <h2 id="files" className="mb-2 flex items-center gap-1.5 text-sm font-semibold">
        <Paperclip aria-hidden className="size-4 text-muted" /> Files
      </h2>
      {r.attachments.length === 0 ? (
        <p className="text-[13px] text-muted">No files yet.</p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {r.attachments.map((a) => (
            <li key={a.id}>
              <a href={a.url} className="group flex items-center gap-2 text-sm hover:text-accent" download>
                <Download aria-hidden className="size-3.5 shrink-0 text-muted group-hover:text-accent" />
                <span className="min-w-0 flex-1 truncate">{a.filename}</span>
                <span className="shrink-0 text-xs text-muted">{formatBytes(a.size)}</span>
              </a>
            </li>
          ))}
        </ul>
      )}
      {r.canComment && (
        <>
          <input
            ref={input}
            id="portal-file"
            type="file"
            hidden
            aria-label="Attach a file"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) upload.mutate(f);
              e.target.value = "";
            }}
          />
          <Button size="sm" className="mt-3" loading={upload.isPending} icon={<Paperclip className="size-3.5" />} onClick={() => input.current?.click()}>
            Attach a file
          </Button>
        </>
      )}
    </section>
  );
}

function ApprovalCard({ request: r, approval: a }: { request: PortalRequestDetail; approval: Approval }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [comment, setComment] = useState("");
  const decide = useMutation({
    mutationFn: (decision: "approve" | "decline") =>
      unwrap(api.POST("/portal/requests/{issueKey}/approval", { params: { path: { issueKey: r.key } }, body: { decision, comment: comment.trim() || undefined } })),
    onSuccess: (_, decision) => {
      qc.invalidateQueries({ queryKey: ["portal"] });
      toast.success(decision === "approve" ? "Approved" : "Declined");
    },
  });
  const statusTone = a.status === "approved" ? "success" : a.status === "declined" ? "danger" : "warning";
  return (
    <section aria-labelledby={`approval-${a.id}`} className="rounded-lg border border-line bg-surface p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id={`approval-${a.id}`} className="flex items-center gap-1.5 text-base font-semibold">
          <ShieldCheck aria-hidden className="size-4.5 text-accent" /> Approval
        </h2>
        <Badge tone={statusTone}>{a.status === "pending" ? "Waiting for approval" : a.status}</Badge>
      </div>
      <p className="mt-1 text-[13px] text-muted">{a.mode === "all" ? "Everyone below must approve." : "One approval is enough."}</p>
      <ul className="mt-3 flex flex-col gap-2">
        {a.approvers.map((p) => (
          <li key={p.person.id} className="flex items-start gap-2 text-sm">
            <UserAvatar user={p.person} size={24} />
            <div className="min-w-0 flex-1">
              <p>
                <span className="font-medium">{p.person.displayName}</span>{" "}
                <span className="text-muted">
                  {p.decision === "approved" ? "approved" : p.decision === "declined" ? "declined" : "has not decided yet"}
                  {p.decidedAt && ` ${relativeTime(p.decidedAt)}`}
                </span>
              </p>
              {p.comment && <p className="mt-0.5 text-[13px] text-muted">“{p.comment}”</p>}
            </div>
          </li>
        ))}
      </ul>
      {a.canDecide && (
        <div className="mt-4 border-t border-line pt-4">
          <label htmlFor={`approval-comment-${a.id}`} className="text-[13px] font-medium">
            Comment (optional)
          </label>
          <Textarea
            id={`approval-comment-${a.id}`}
            rows={2}
            className="mt-1.5"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Why you approve or decline…"
          />
          {decide.error ? <ErrorBox error={decide.error} className="mt-2" /> : null}
          <div className="mt-2 flex flex-wrap gap-2">
            <Button
              variant="primary"
              icon={<CheckCircle2 className="size-4" />}
              loading={decide.isPending && decide.variables === "approve"}
              onClick={() => decide.mutate("approve")}
            >
              Approve
            </Button>
            <Button variant="danger" icon={<CircleX className="size-4" />} loading={decide.isPending && decide.variables === "decline"} onClick={() => decide.mutate("decline")}>
              Decline
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
