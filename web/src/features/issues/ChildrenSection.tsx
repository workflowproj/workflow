import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { hasPermission, issueTypesQuery, useProject } from "@/api/queries";
import type { Issue } from "@/api/types";
import { Button, IconButton, useToast } from "@/components/ui";
import { IssueTypeIcon, PriorityIcon, StatusBadge } from "@/components/issue-bits";
import { UserAvatar } from "@/components/UserAvatar";
import { cn } from "@/lib/cn";
import { invalidateIssueLists } from "./issue-api";
import { useEscapeLayer } from "@/lib/escape";
import { IssueKeyLink, Section, useIssue, useIssueAction } from "./issue-context";

/** ChildrenSection lists sub-tasks (of a standard issue) or child issues (of an epic). */
export function ChildrenSection() {
  const { issue } = useIssue();
  const { data: project } = useProject(issue.projectKey);
  const { data: types = [] } = useQuery(issueTypesQuery(issue.projectKey));
  const childTypes = types.filter((t) => t.level === issue.type.level - 1);
  const [adding, setAdding] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const canCreate = hasPermission(project, "issue.create") && childTypes.length > 0;
  useIssueAction("add-child", () => setAdding(true));
  useEffect(() => {
    if (adding) ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [adding]);
  const children = issue.children;
  // Empty lists stay out of the way; "Add sub-task" in the toolbar opens the form.
  if (issue.type.level < 0 || (children.length === 0 && !adding)) return null;

  const title = issue.type.level === 0 ? "Sub-tasks" : "Child issues";
  return (
    <div ref={ref} className="min-w-0 scroll-mt-16">
      <Section
        title={title}
        count={children.length}
        actions={
          canCreate &&
          !adding && (
            <IconButton label={issue.type.level === 0 ? "Add sub-task" : "Add child issue"} size="sm" onClick={() => setAdding(true)}>
              <Plus className="size-4" />
            </IconButton>
          )
        }
      >
        {children.length > 0 && <Progress items={children} />}
        {children.length > 0 && (
          <ul className="mt-2 overflow-hidden rounded-md border border-line">
            {children.map((c) => (
              <ChildRow key={c.id} child={c} />
            ))}
          </ul>
        )}
        {adding && <AddChild types={childTypes} onDone={() => setAdding(false)} />}
      </Section>
    </div>
  );
}

function Progress({ items }: { items: Issue[] }) {
  const { issue } = useIssue();
  const total = issue.progress?.total ?? items.length;
  const done = issue.progress?.done ?? items.filter((c) => c.status.category === "done").length;
  const doing = issue.progress?.inProgress ?? items.filter((c) => c.status.category === "in_progress").length;
  const pct = (n: number) => (total ? (n / total) * 100 : 0);
  const points = issue.progress && issue.progress.points > 0 ? ` · ${fmt(issue.progress.donePoints)} of ${fmt(issue.progress.points)} points` : "";
  return (
    <div className="flex items-center gap-3">
      <div
        className="flex h-1.5 flex-1 overflow-hidden rounded-full bg-surface-3"
        role="progressbar"
        aria-label="Done"
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={done}
      >
        <div className="bg-done transition-all" style={{ width: `${pct(done)}%` }} />
        <div className="bg-progress transition-all" style={{ width: `${pct(doing)}%` }} />
      </div>
      <span className="shrink-0 text-xs text-muted">
        {Math.round(pct(done))}% done
        <span className="text-faint">
          {" "}
          · {done} of {total}
          {points}
        </span>
      </span>
    </div>
  );
}

const fmt = (n: number) => String(Math.round(n * 10) / 10);

function ChildRow({ child }: { child: Issue }) {
  const done = child.status.category === "done";
  return (
    <li className="flex items-center gap-2.5 border-b border-line px-3 py-2 text-sm last:border-b-0 hover:bg-surface-2">
      <IssueTypeIcon type={child.type} />
      <IssueKeyLink issueKey={child.key} className={cn("shrink-0 font-mono text-xs text-muted", done && "line-through")} />
      <IssueKeyLink issueKey={child.key} className="min-w-0 flex-1 truncate text-ink">
        {child.summary}
      </IssueKeyLink>
      <PriorityIcon priority={child.priority} className="hidden sm:block" />
      {child.storyPoints != null && <span className="hidden rounded-full bg-surface-3 px-1.5 text-[11px] font-semibold text-muted sm:inline">{child.storyPoints}</span>}
      <UserAvatar user={child.assignee} size={20} />
      <StatusBadge status={child.status} className="w-24 justify-center" />
    </li>
  );
}

function AddChild({ types, onDone }: { types: { key: string; name: string; icon: string; color: string }[]; onDone: () => void }) {
  const { issue, open } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const [summary, setSummary] = useState("");
  const [typeKey, setTypeKey] = useState(types[0]?.key ?? "");
  const input = useRef<HTMLInputElement>(null);
  const create = useMutation({
    mutationFn: (s: string) => unwrap(api.POST("/issues", { body: { projectKey: issue.projectKey, typeKey, summary: s, parentKey: issue.key } })),
    onSuccess: (created) => {
      setSummary("");
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      invalidateIssueLists(qc);
      toast.success(`${created.key} created`, { label: "Open", onClick: () => open(created.key) });
      input.current?.focus();
    },
    onError: (err) => toast.error(err),
  });
  const submit = () => summary.trim() && create.mutate(summary.trim());
  useEscapeLayer(true, onDone);
  const type = types.find((t) => t.key === typeKey);
  return (
    <form
      className="mt-2 flex items-center gap-2 rounded-md border border-accent bg-surface p-1.5 ring-2 ring-accent/20"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      {types.length > 1 ? (
        <select
          aria-label="Issue type"
          value={typeKey}
          onChange={(e) => setTypeKey(e.target.value)}
          className="h-7 w-28 shrink-0 rounded border-0 bg-surface-2 px-1.5 text-[13px] text-ink focus:ring-2 focus:ring-accent/25 focus:outline-none"
        >
          {types.map((t) => (
            <option key={t.key} value={t.key}>
              {t.name}
            </option>
          ))}
        </select>
      ) : (
        type && <IssueTypeIcon type={type} className="ml-1" />
      )}
      <input
        ref={input}
        autoFocus
        value={summary}
        onChange={(e) => setSummary(e.target.value)}
        maxLength={255}
        placeholder="What needs to be done?"
        aria-label="Summary"
        className="h-7 min-w-0 flex-1 bg-transparent px-1 text-sm outline-none placeholder:text-faint"
      />
      <Button size="sm" variant="primary" type="submit" loading={create.isPending} disabled={!summary.trim()}>
        Create
      </Button>
      <Button size="sm" variant="ghost" onClick={onDone}>
        Cancel
      </Button>
    </form>
  );
}
