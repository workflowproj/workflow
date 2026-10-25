import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CircleX, ExternalLink, ShieldCheck, UserPlus, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, IconButton, Input, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { relativeTime } from "@/lib/format";
import { useIssue } from "@/features/issues/issue-context";
import { requestTypesQuery, serviceRequestQuery, type Approval, type ServiceRequest } from "./api";
import { SlaBadge } from "./bits";
import { formatSeconds } from "./sla";

/**
 * RequestPanel is the service-request side of an issue in a service project: its request type
 * (what makes it visible in the help centre), SLA clocks, approvals and the people it is shared
 * with.
 */
export function RequestPanel() {
  const { issue } = useIssue();
  const q = useQuery(serviceRequestQuery(issue.key));
  const r = q.data;
  return (
    <section aria-labelledby="request-panel" className="rounded-lg border border-line bg-surface">
      <div className="flex items-center justify-between gap-2 border-b border-line px-4 py-2.5">
        <h2 id="request-panel" className="text-sm font-semibold">
          Service request
        </h2>
        {r?.customerVisible && (
          <a href={r.portalUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-xs text-muted hover:text-ink">
            <ExternalLink aria-hidden className="size-3.5" /> Help centre view
          </a>
        )}
      </div>
      {q.error && <ErrorBox error={q.error} className="m-3" />}
      {r && (
        <div className="flex flex-col gap-4 px-4 py-3">
          <RequestType request={r} />
          <div>
            <h3 className="mb-1.5 text-[13px] text-muted">SLAs</h3>
            {r.slas.length === 0 ? (
              <p className="text-[13px] text-faint">No SLA applies.</p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {r.slas.map((s) => (
                  <li key={s.metric} className="flex flex-wrap items-center justify-between gap-x-2 gap-y-0.5 text-sm">
                    <span>{s.name}</span>
                    <SlaBadge sla={s} />
                    <span className="w-full text-xs text-muted">
                      Goal {formatSeconds(s.targetSeconds)}
                      {s.calendar ? ` · ${s.calendar}` : " · around the clock"}
                      {s.state === "paused" && " · paused"}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {r.approvals.map((a) => (
            <ApprovalBlock key={a.id} approval={a} />
          ))}
          <Participants request={r} />
        </div>
      )}
    </section>
  );
}

function RequestType({ request: r }: { request: ServiceRequest }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const { data: types = [] } = useQuery(requestTypesQuery(issue.projectKey));
  const set = useMutation({
    mutationFn: (requestTypeId: string) => unwrap(api.PATCH("/issues/{issueKey}/request", { params: { path: { issueKey: issue.key } }, body: { requestTypeId } })),
    onSuccess: (data) => {
      qc.setQueryData(["issue", issue.key, "request"], data);
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
    },
    onError: toast.error,
  });
  return (
    <div>
      <label htmlFor="request-type" className="mb-1 block text-[13px] text-muted">
        Request type
      </label>
      <Select id="request-type" disabled={!issue.permissions.edit || set.isPending} value={r.requestType?.id ?? ""} onChange={(e) => e.target.value && set.mutate(e.target.value)}>
        {!r.requestType && <option value="">None: not shown to the customer</option>}
        {types.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </Select>
      {r.customerVisible && issue.reporter && (
        <p className="mt-1 text-xs text-muted">
          {issue.reporter.displayName} follows it in the help centre{r.participants.length ? `, with ${r.participants.length} other${r.participants.length === 1 ? "" : "s"}` : ""}.
        </p>
      )}
    </div>
  );
}

function ApprovalBlock({ approval: a }: { approval: Approval }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const [comment, setComment] = useState("");
  const decide = useMutation({
    mutationFn: (decision: "approve" | "decline") =>
      unwrap(
        api.POST("/issues/{issueKey}/approvals/{approvalId}", {
          params: { path: { issueKey: issue.key, approvalId: a.id } },
          body: { decision, comment: comment.trim() || undefined },
        }),
      ),
    onSuccess: (data, decision) => {
      qc.setQueryData(["issue", issue.key, "request"], data);
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
      toast.success(decision === "approve" ? "Approved" : "Declined");
    },
    onError: toast.error,
  });
  const tone = a.status === "approved" ? "success" : a.status === "declined" ? "danger" : "warning";
  return (
    <div>
      <h3 className="mb-1.5 flex items-center justify-between gap-2 text-[13px] text-muted">
        <span className="flex items-center gap-1">
          <ShieldCheck aria-hidden className="size-3.5" /> Approval ({a.mode === "all" ? "all must approve" : "any one"})
        </span>
        <Badge tone={tone}>{a.status}</Badge>
      </h3>
      <ul className="flex flex-col gap-1">
        {a.approvers.map((p) => (
          <li key={p.person.id} className="flex items-start gap-2 text-sm">
            <UserAvatar user={p.person} size={20} className="mt-0.5" />
            <span className="min-w-0 flex-1">
              {p.person.displayName}{" "}
              <span className="text-xs text-muted">
                {p.decision ? `${p.decision} ${p.decidedAt ? relativeTime(p.decidedAt) : ""}` : "pending"}
                {p.comment && ` — “${p.comment}”`}
              </span>
            </span>
          </li>
        ))}
      </ul>
      {a.canDecide && (
        <div className="mt-2 flex flex-col gap-2">
          <label htmlFor={`agent-approval-${a.id}`} className="sr-only">
            Approval comment
          </label>
          <Textarea id={`agent-approval-${a.id}`} rows={2} value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Comment (optional)" />
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="primary"
              icon={<CheckCircle2 className="size-3.5" />}
              loading={decide.isPending && decide.variables === "approve"}
              onClick={() => decide.mutate("approve")}
            >
              Approve
            </Button>
            <Button
              size="sm"
              variant="danger"
              icon={<CircleX className="size-3.5" />}
              loading={decide.isPending && decide.variables === "decline"}
              onClick={() => decide.mutate("decline")}
            >
              Decline
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function Participants({ request: r }: { request: ServiceRequest }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [adding, setAdding] = useState(false);
  const add = useMutation({
    mutationFn: () => unwrap(api.POST("/issues/{issueKey}/participants", { params: { path: { issueKey: issue.key } }, body: { email: email.trim() } })),
    onSuccess: (data) => {
      qc.setQueryData(["issue", issue.key, "request"], data);
      setEmail("");
      setAdding(false);
    },
  });
  const remove = useMutation({
    mutationFn: (userId: string) => unwrap(api.DELETE("/issues/{issueKey}/participants/{userId}", { params: { path: { issueKey: issue.key, userId } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue", issue.key, "request"] }),
    onError: toast.error,
  });
  if (!r.customerVisible && r.participants.length === 0) return null;
  return (
    <div>
      <h3 className="mb-1.5 flex items-center justify-between text-[13px] text-muted">
        Shared with
        {issue.permissions.edit && !adding && (
          <button type="button" onClick={() => setAdding(true)} className="inline-flex items-center gap-1 text-xs text-accent hover:underline">
            <UserPlus aria-hidden className="size-3.5" /> Share
          </button>
        )}
      </h3>
      {r.participants.length === 0 && !adding && <p className="text-[13px] text-faint">Nobody else.</p>}
      <ul className="flex flex-col gap-1">
        {r.participants.map((u) => (
          <li key={u.id} className="flex items-center gap-2 text-sm">
            <UserAvatar user={u} size={20} />
            <span className="min-w-0 flex-1 truncate">{u.displayName}</span>
            {issue.permissions.edit && (
              <IconButton size="sm" label={`Stop sharing with ${u.displayName}`} onClick={() => remove.mutate(u.id)}>
                <X className="size-3.5" />
              </IconButton>
            )}
          </li>
        ))}
      </ul>
      {adding && (
        <form
          className="mt-2 flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (email.trim()) add.mutate();
          }}
        >
          <label htmlFor="participant-email" className="sr-only">
            Email of the person to share with
          </label>
          <Input id="participant-email" type="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@example.com" />
          {add.error ? <ErrorBox error={add.error} /> : null}
          <div className="flex gap-2">
            <Button size="sm" type="submit" variant="primary" loading={add.isPending} disabled={!email.trim()}>
              Share
            </Button>
            <Button size="sm" variant="ghost" onClick={() => (setAdding(false), setEmail(""), add.reset())}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
