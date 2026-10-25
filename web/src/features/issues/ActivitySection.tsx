import { useState } from "react";
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { ArrowRight, Lock, MessageSquare, Reply } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import { useMe, useProject } from "@/api/queries";
import { Badge, Button, PageSpinner, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Markdown } from "@/components/Markdown";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { formatDate, formatDateTime, relativeTime } from "@/lib/format";
import { commentsQuery, historyQuery } from "./issue-api";
import { useEscapeLayer } from "@/lib/escape";
import { useIssue } from "./issue-context";
import { worklogsQuery } from "@/features/time/api";
import { WorklogList } from "@/features/time/WorklogList";

type Comment = Schemas["Comment"];
type HistoryEntry = Schemas["HistoryEntry"];

/** ActivitySection holds the Comments, Work log and History tabs. */
export function ActivitySection() {
  const { issue } = useIssue();
  const [tab, setTab] = useState<"comments" | "worklog" | "history">("comments");
  const comments = useQuery(commentsQuery(issue.key));
  const worklogs = useQuery(worklogsQuery(issue.key));
  const tabs = [
    { id: "comments" as const, label: "Comments", count: comments.data?.length },
    { id: "worklog" as const, label: "Work log", count: worklogs.data?.length },
    { id: "history" as const, label: "History" },
  ];
  return (
    <section className="min-w-0">
      <div className="mb-4 flex items-center gap-4 border-b border-line">
        <h2 className="sr-only">Activity</h2>
        <div role="tablist" aria-label="Activity" className="-mb-px flex gap-1">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={tab === t.id}
              onClick={() => setTab(t.id)}
              className={cn(
                "flex items-center gap-1.5 border-b-2 px-2.5 py-2 text-sm",
                tab === t.id ? "border-accent font-medium text-ink" : "border-transparent text-muted hover:text-ink",
              )}
            >
              {t.label}
              {!!t.count && <span className="rounded-full bg-surface-2 px-1.5 text-[11px] font-semibold text-muted">{t.count}</span>}
            </button>
          ))}
        </div>
      </div>
      <div role="tabpanel">{tab === "comments" ? <Comments query={comments} /> : tab === "worklog" ? <WorklogList issue={issue} /> : <History />}</div>
    </section>
  );
}

const sourceLabel: Record<string, string> = { automation: "via automation", scm: "via commit" };

function Comments({ query }: { query: UseQueryResult<Comment[]> }) {
  const { issue } = useIssue();
  if (query.isLoading) return <PageSpinner label="Loading comments" />;
  if (query.error) return <ErrorBox error={query.error} />;
  const list = query.data ?? [];
  return (
    <div className="flex flex-col gap-5">
      {list.length === 0 && (
        <p className="flex items-center gap-2 text-sm text-faint">
          <MessageSquare className="size-4" /> No comments yet{issue.permissions.comment ? " — start the conversation below." : "."}
        </p>
      )}
      {list.length > 0 && (
        <ol className="flex flex-col gap-5">
          {list.map((c) => (
            <CommentItem key={c.id} comment={c} />
          ))}
        </ol>
      )}
      {issue.permissions.comment && <Composer />}
    </div>
  );
}

const automationUser = { displayName: "Automation", kind: "automation" };

function CommentItem({ comment: c }: { comment: Comment }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [draft, setDraft] = useState<string | null>(null);
  const key = ["issue-comments", issue.key];
  const save = useMutation({
    mutationFn: (body: string) => unwrap(api.PATCH("/comments/{commentId}", { params: { path: { commentId: c.id } }, body: { body } })),
    onSuccess: (updated) => {
      qc.setQueryData<Comment[]>(key, (list) => list?.map((x) => (x.id === c.id ? { ...x, ...updated, canEdit: x.canEdit } : x)));
      setDraft(null);
    },
    onError: (err) => toast.error(err),
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/comments/{commentId}", { params: { path: { commentId: c.id } } })),
    onSuccess: () => {
      qc.setQueryData<Comment[]>(key, (list) => list?.filter((x) => x.id !== c.id));
      qc.invalidateQueries({ queryKey: key });
    },
    onError: (err) => toast.error(err),
  });
  // While editing, Escape cancels the edit rather than closing a surrounding drawer.
  useEscapeLayer(draft !== null, () => draft?.trim() === c.body.trim() && setDraft(null));
  const author = c.author ?? (c.source === "automation" ? automationUser : { displayName: "Unknown", kind: "human" });
  const via = c.via ? `via ${c.via}` : c.source !== "user" ? (sourceLabel[c.source] ?? `via ${c.source}`) : null;
  const submit = () => draft?.trim() && save.mutate(draft.trim());
  return (
    <li className="flex gap-3">
      <UserAvatar user={author} size={28} className="mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
          <span className="font-medium">{author.displayName}</span>
          {via && <Badge tone={c.source === "automation" ? "accent" : "neutral"}>{via}</Badge>}
          {c.visibility === "internal" && (
            <Badge tone="warning">
              <Lock aria-hidden className="size-3" /> Internal note
            </Badge>
          )}
          {c.visibility === "public" && c.source !== "portal" && (
            <Badge tone="success">
              <Reply aria-hidden className="size-3" /> Reply to customer
            </Badge>
          )}
          <time dateTime={c.createdAt} title={formatDateTime(c.createdAt)} className="text-xs text-muted">
            {relativeTime(c.createdAt)}
          </time>
          {c.editedAt && (
            <span className="text-xs text-faint" title={`Edited ${formatDateTime(c.editedAt)}`}>
              (edited)
            </span>
          )}
        </div>
        {draft !== null ? (
          <div className="mt-1.5">
            <MarkdownEditor value={draft} onChange={setDraft} autoFocus onSubmit={submit} minHeight={72} />
            <div className="mt-2 flex gap-2">
              <Button size="sm" variant="primary" onClick={submit} loading={save.isPending} disabled={!draft.trim()}>
                Save
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <>
            <Markdown className={cn("mt-1", c.visibility === "internal" && "rounded-md border-l-2 border-warning bg-warning-soft/40 px-3 py-1.5")}>{c.body}</Markdown>
            {c.canEdit && (
              <div className="mt-1 flex gap-3 text-xs text-muted">
                <button type="button" className="hover:text-ink hover:underline" onClick={() => setDraft(c.body)}>
                  Edit
                </button>
                <button
                  type="button"
                  className="hover:text-danger hover:underline"
                  disabled={remove.isPending}
                  onClick={async () => {
                    if (await confirm({ title: "Delete comment?", body: "This comment will be removed for everyone.", confirmLabel: "Delete", danger: true })) remove.mutate();
                  }}
                >
                  Delete
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </li>
  );
}

type Visibility = Schemas["Visibility"];

function Composer() {
  const { issue } = useIssue();
  const { data: me } = useMe();
  const { data: project } = useProject(issue.projectKey);
  const qc = useQueryClient();
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const [body, setBody] = useState("");
  // Service requests: a reply reaches the customer, an internal note only agents.
  const service = project?.template === "service";
  const [visibility, setVisibility] = useState<Visibility>("internal");
  const start = (v?: Visibility) => {
    if (v) setVisibility(v);
    setOpen(true);
  };
  const post = useMutation({
    mutationFn: (text: string) =>
      unwrap(api.POST("/issues/{issueKey}/comments", { params: { path: { issueKey: issue.key } }, body: service ? { body: text, visibility } : { body: text } })),
    onSuccess: (c) => {
      qc.setQueryData<Comment[]>(["issue-comments", issue.key], (list) => [...(list ?? []).filter((x) => x.id !== c.id), { ...c, canEdit: true }]);
      qc.invalidateQueries({ queryKey: ["issue-comments", issue.key] });
      if (service) qc.invalidateQueries({ queryKey: ["issue", issue.key, "request"] });
      setBody("");
      setOpen(false);
    },
    onError: (err) => toast.error(err),
  });
  const submit = () => body.trim() && !post.isPending && post.mutate(body.trim());
  const internal = service && visibility === "internal";
  // Escape collapses an empty composer; a draft is kept (and the drawer stays open).
  useEscapeLayer(open, () => !body.trim() && setOpen(false));
  return (
    <div className="flex gap-3">
      <UserAvatar user={me} size={28} className="mt-0.5" />
      <div className="min-w-0 flex-1">
        {open ? (
          <div className={cn(internal && "rounded-lg border-l-2 border-warning bg-warning-soft/40 p-2")}>
            {service && <VisibilityToggle value={visibility} onChange={setVisibility} />}
            <MarkdownEditor
              value={body}
              onChange={setBody}
              autoFocus
              onSubmit={submit}
              minHeight={80}
              placeholder={!service ? "Add a comment… Markdown is supported" : internal ? "Only agents will see this note…" : "Reply to the customer… they get it by email too"}
            />
            <div className="mt-2 flex items-center gap-2">
              <Button size="sm" variant="primary" onClick={submit} loading={post.isPending} disabled={!body.trim()}>
                {!service ? "Comment" : internal ? "Add internal note" : "Send reply"}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setBody("");
                  setOpen(false);
                }}
              >
                Cancel
              </Button>
              <span className="ml-auto hidden text-xs text-faint sm:inline">⌘/Ctrl + Enter to send</span>
            </div>
          </div>
        ) : service ? (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" icon={<Lock className="size-3.5" />} onClick={() => start("internal")}>
              Add internal note
            </Button>
            <Button size="sm" icon={<Reply className="size-3.5" />} onClick={() => start("public")}>
              Reply to customer
            </Button>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => start()}
            className="h-9 w-full rounded-md border border-line-strong bg-surface px-3 text-left text-sm text-faint hover:border-accent"
          >
            Add a comment…
          </button>
        )}
      </div>
    </div>
  );
}

/** VisibilityToggle switches a service request comment between an internal note and a reply to the customer. */
function VisibilityToggle({ value, onChange }: { value: Visibility; onChange: (v: Visibility) => void }) {
  const options = [
    { v: "internal" as const, label: "Internal note", icon: Lock },
    { v: "public" as const, label: "Reply to customer", icon: Reply },
  ];
  return (
    <div role="radiogroup" aria-label="Who sees this comment" className="mb-2 inline-flex rounded-md border border-line-strong bg-surface p-0.5">
      {options.map((o) => (
        <button
          key={o.v}
          type="button"
          role="radio"
          aria-checked={value === o.v}
          onClick={() => onChange(o.v)}
          className={cn(
            "inline-flex h-7 items-center gap-1.5 rounded px-2.5 text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-accent",
            value === o.v ? (o.v === "internal" ? "bg-warning-soft text-warning" : "bg-success-soft text-success") : "text-muted hover:text-ink",
          )}
        >
          <o.icon aria-hidden className="size-3.5" />
          {o.label}
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// History

const FIELD_LABELS: Record<string, string> = {
  status: "status",
  assignee: "assignee",
  reporter: "reporter",
  priority: "priority",
  summary: "summary",
  sprint: "sprint",
  storyPoints: "story points",
  startDate: "start date",
  dueDate: "due date",
  labels: "labels",
  fixVersions: "fix versions",
  parent: "parent",
  type: "type",
  resolution: "resolution",
  originalEstimate: "original estimate",
  remainingEstimate: "remaining estimate",
  requestType: "request type",
};

// Service requests: what kind of comment was added (the history keeps no comment text).
const COMMENT_KINDS: Record<string, string> = {
  "Internal note": "added an internal note",
  "Reply to customer": "replied to the customer",
  "Reply from the portal": "commented from the help centre",
};

const actorNames: Record<string, string> = { automation: "Automation", system: "System", scm: "Git integration" };

function History() {
  const { issue } = useIssue();
  const { data, isLoading, error } = useQuery(historyQuery(issue.key));
  if (isLoading) return <PageSpinner label="Loading history" />;
  if (error) return <ErrorBox error={error} />;
  const entries = data ?? [];
  if (entries.length === 0) return <p className="text-sm text-faint">No changes recorded yet.</p>;
  return (
    <ol className="relative flex flex-col gap-4 before:absolute before:top-2 before:bottom-2 before:left-[11px] before:w-px before:bg-line">
      {entries.map((h) => (
        <li key={h.id} className="relative flex gap-3">
          <UserAvatar user={h.actor ?? { displayName: actorNames[h.actorType] ?? "System", kind: "automation" }} size={24} className="ring-4 ring-surface" />
          <div className="min-w-0 flex-1 pt-0.5 text-sm">
            <span className="font-medium">{h.actor?.displayName ?? actorNames[h.actorType] ?? "System"}</span>
            {h.via && <span className="text-muted"> via {h.via}</span>} <Change entry={h} />
            <time dateTime={h.createdAt} title={formatDateTime(h.createdAt)} className="ml-2 text-xs text-muted">
              {relativeTime(h.createdAt)}
            </time>
          </div>
        </li>
      ))}
    </ol>
  );
}

function Value({ v, field }: { v?: string; field: string }) {
  if (!v) return <span className="text-faint italic">none</span>;
  let text = v;
  if (field === "priority") text = v.charAt(0).toUpperCase() + v.slice(1);
  else if (/^\d{4}-\d{2}-\d{2}$/.test(v)) text = formatDate(v);
  return <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[13px] break-words">{text}</span>;
}

function Change({ entry: h }: { entry: HistoryEntry }) {
  switch (h.field) {
    case "created":
      return <span className="text-muted">created the issue</span>;
    case "description":
      return <span className="text-muted">updated the description</span>;
    case "attachment":
      return h.to ? (
        <span className="text-muted">
          attached <Value v={h.to} field={h.field} />
        </span>
      ) : (
        <span className="text-muted">
          removed attachment <Value v={h.from} field={h.field} />
        </span>
      );
    case "worklog":
      return h.to && !h.from ? (
        <span className="text-muted">
          logged work <Value v={h.to} field={h.field} />
        </span>
      ) : h.to ? (
        <span className="text-muted">
          changed logged work{" "}
          <span className="inline-flex flex-wrap items-center gap-1.5 align-middle">
            <Value v={h.from} field={h.field} />
            <ArrowRight className="size-3 text-faint" />
            <Value v={h.to} field={h.field} />
          </span>
        </span>
      ) : (
        <span className="text-muted">
          deleted logged work <Value v={h.from} field={h.field} />
        </span>
      );
    case "comment":
      return <span className="text-muted">{COMMENT_KINDS[h.to ?? ""] ?? "commented"}</span>;
    case "approval":
      return (
        <span className="text-muted">
          updated the approval: <Value v={h.to} field={h.field} />
        </span>
      );
    case "participants":
      return h.to ? (
        <span className="text-muted">
          shared the request with <Value v={h.to} field={h.field} />
        </span>
      ) : (
        <span className="text-muted">
          stopped sharing the request with <Value v={h.from} field={h.field} />
        </span>
      );
    case "link":
      return h.to ? (
        <span className="text-muted">
          linked this issue: <Value v={h.to} field={h.field} />
        </span>
      ) : (
        <span className="text-muted">
          removed the link <Value v={h.from} field={h.field} />
        </span>
      );
  }
  const label = FIELD_LABELS[h.field] ?? h.field;
  return (
    <span className="text-muted">
      changed the <span className="text-ink">{label}</span>{" "}
      <span className="inline-flex flex-wrap items-center gap-1.5 align-middle">
        <Value v={h.from} field={h.field} />
        <ArrowRight className="size-3 text-faint" />
        <Value v={h.to} field={h.field} />
      </span>
    </span>
  );
}
