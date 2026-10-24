import { useState, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, Check, CircleCheck, CircleX, Clock, Copy, GitBranch, GitCommitHorizontal, GitPullRequest, LoaderCircle, MessageSquare, TriangleAlert } from "lucide-react";
import { ApiError, api, unwrap, type Schemas } from "@/api/client";
import { hasPermission, useProject } from "@/api/queries";
import { Badge, Button, Field, Input, Modal, Select, useToast, type Tone } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { formatDateTime, relativeTime } from "@/lib/format";
import { repositoriesQuery } from "./issue-api";
import { Section, useIssue } from "./issue-context";

type DevLink = Schemas["DevLink"];

const prTone: Record<string, Tone> = { open: "success", draft: "neutral", merged: "accent", closed: "danger" };
const prIcon: Record<string, string> = { open: "text-success", draft: "text-muted", merged: "text-accent", closed: "text-danger" };

const byRecent = (a: DevLink, b: DevLink) => b.updatedAt.localeCompare(a.updatedAt);

/** DevelopmentPanel summarises branches, commits, pull requests and pipelines linked to the issue. */
export function DevelopmentPanel() {
  const { issue } = useIssue();
  const { data: project } = useProject(issue.projectKey);
  const { data: repos, isLoading } = useQuery(repositoriesQuery(issue.projectKey));
  const [creating, setCreating] = useState(false);
  const [allCommits, setAllCommits] = useState(false);
  const dev = issue.development;
  const prs = dev.filter((d) => d.kind === "pull_request").sort(byRecent);
  const branches = dev.filter((d) => d.kind === "branch" && d.state !== "deleted").sort(byRecent);
  const commits = dev.filter((d) => d.kind === "commit").sort(byRecent);
  const pipeline = dev.filter((d) => d.kind === "pipeline").sort(byRecent)[0];
  const hasRepos = (repos?.length ?? 0) > 0;
  const canBranch = issue.permissions.edit && hasRepos;

  return (
    <Section
      title="Development"
      actions={
        canBranch && (
          <Button size="sm" variant="ghost" icon={<GitBranch className="size-3.5" />} onClick={() => setCreating(true)}>
            Create branch
          </Button>
        )
      }
    >
      {dev.length === 0 ? (
        <p className="text-sm text-muted">
          {isLoading ? (
            "Loading…"
          ) : hasRepos ? (
            <>
              No branches, commits or pull requests yet. Create a branch, or mention <span className="font-mono text-xs text-ink">{issue.key}</span> in a branch name, commit
              message or pull request title.
            </>
          ) : hasPermission(project, "project.admin") ? (
            <>
              <Link to="/projects/$projectKey/settings/repositories" params={{ projectKey: issue.projectKey }} className="text-accent underline underline-offset-2">
                Connect a repository in project settings
              </Link>{" "}
              to see branches, commits and pull requests here.
            </>
          ) : (
            "Connect a repository in project settings to see branches, commits and pull requests here. Ask a project admin to set it up."
          )}
        </p>
      ) : (
        <div className="overflow-hidden rounded-md border border-line">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-line bg-surface-2/60 px-3 py-2 text-[13px] text-muted">
            <Count icon={<GitPullRequest className="size-3.5" />} n={prs.length} one="pull request" />
            <Count icon={<GitBranch className="size-3.5" />} n={branches.length} one="branch" many="branches" />
            <Count icon={<GitCommitHorizontal className="size-3.5" />} n={commits.length} one="commit" />
            {pipeline && (
              <a
                href={pipeline.url}
                target="_blank"
                rel="noreferrer noopener"
                className="ml-auto flex items-center gap-1.5 hover:text-ink"
                title={`${pipeline.title} · ${formatDateTime(pipeline.updatedAt)}`}
              >
                <PipelineIcon state={pipeline.state} />
                Pipeline {pipeline.state}
              </a>
            )}
          </div>
          {prs.length > 0 && (
            <Group label="Pull requests">
              {prs.map((pr) => (
                <PullRequestRow key={pr.id} pr={pr} />
              ))}
            </Group>
          )}
          {branches.length > 0 && (
            <Group label="Branches">
              {branches.map((b) => (
                <li key={b.id} className="flex items-center gap-2 px-3 py-1.5 text-sm">
                  <GitBranch className="size-3.5 shrink-0 text-faint" />
                  <a href={b.url} target="_blank" rel="noreferrer noopener" className="min-w-0 truncate font-mono text-[13px] hover:text-accent">
                    {b.externalId}
                  </a>
                  <span className="ml-auto shrink-0 truncate text-xs text-faint">{b.repository.fullPath}</span>
                </li>
              ))}
            </Group>
          )}
          {commits.length > 0 && (
            <Group label="Commits">
              {(allCommits ? commits : commits.slice(0, 5)).map((c) => (
                <li key={c.id} className="flex items-center gap-2 px-3 py-1.5 text-sm">
                  <a href={c.url} target="_blank" rel="noreferrer noopener" className="shrink-0 rounded bg-surface-2 px-1.5 font-mono text-xs text-muted hover:text-accent">
                    {c.externalId.slice(0, 7)}
                  </a>
                  <span className="min-w-0 flex-1 truncate" title={c.title}>
                    {c.title.split("\n")[0]}
                  </span>
                  <span className="hidden shrink-0 text-xs text-faint sm:inline" title={formatDateTime(c.updatedAt)}>
                    {c.authorName && `${c.authorName} · `}
                    {relativeTime(c.updatedAt)}
                  </span>
                </li>
              ))}
              {commits.length > 5 && (
                <li className="px-3 py-1.5">
                  <button type="button" onClick={() => setAllCommits((v) => !v)} className="text-xs text-accent hover:underline">
                    {allCommits ? "Show fewer" : `Show all ${commits.length} commits`}
                  </button>
                </li>
              )}
            </Group>
          )}
        </div>
      )}
      {creating && repos && <CreateBranchDialog repos={repos} onClose={() => setCreating(false)} />}
    </Section>
  );
}

function Count({ icon, n, one, many }: { icon: ReactNode; n: number; one: string; many?: string }) {
  return (
    <span className={cn("flex items-center gap-1.5", n === 0 && "text-faint")}>
      {icon}
      {n} {n === 1 ? one : (many ?? `${one}s`)}
    </span>
  );
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="border-b border-line py-1 last:border-b-0">
      <p className="px-3 pt-1 pb-0.5 text-[11px] font-semibold tracking-wide text-faint uppercase">{label}</p>
      <ul>{children}</ul>
    </div>
  );
}

function PullRequestRow({ pr }: { pr: DevLink }) {
  const review = typeof pr.data.reviewState === "string" ? pr.data.reviewState : undefined;
  const branch = typeof pr.data.sourceBranch === "string" ? pr.data.sourceBranch : undefined;
  return (
    <li className="flex items-start gap-2 px-3 py-2 text-sm">
      <GitPullRequest className={cn("mt-0.5 size-4 shrink-0", prIcon[pr.state] ?? "text-muted")} />
      <div className="min-w-0 flex-1">
        <a href={pr.url} target="_blank" rel="noreferrer noopener" className="block truncate font-medium hover:text-accent" title={pr.title}>
          {pr.title}
        </a>
        <p className="truncate text-xs text-muted">
          {pr.repository.fullPath}#{pr.externalId}
          {pr.authorName && ` · ${pr.authorName}`}
          {branch && ` · ${branch}`} · <span title={formatDateTime(pr.updatedAt)}>{relativeTime(pr.updatedAt)}</span>
        </p>
      </div>
      <div className="flex shrink-0 flex-col items-end gap-1">
        <Badge tone={prTone[pr.state] ?? "neutral"}>{pr.state}</Badge>
        {review && pr.state !== "merged" && pr.state !== "closed" && <ReviewState state={review} />}
      </div>
    </li>
  );
}

function ReviewState({ state }: { state: string }) {
  if (state === "approved")
    return (
      <span className="flex items-center gap-1 text-xs text-success">
        <Check className="size-3" /> Approved
      </span>
    );
  if (state === "changes_requested")
    return (
      <span className="flex items-center gap-1 text-xs text-warning">
        <TriangleAlert className="size-3" /> Changes requested
      </span>
    );
  return (
    <span className="flex items-center gap-1 text-xs text-muted">
      <MessageSquare className="size-3" /> Reviewed
    </span>
  );
}

function PipelineIcon({ state }: { state: string }) {
  switch (state) {
    case "success":
      return <CircleCheck className="size-3.5 text-success" />;
    case "failed":
      return <CircleX className="size-3.5 text-danger" />;
    case "running":
      return <LoaderCircle className="size-3.5 animate-spin text-progress" />;
    case "canceled":
      return <Ban className="size-3.5 text-muted" />;
    default:
      return <Clock className="size-3.5 text-warning" />;
  }
}

function CreateBranchDialog({ repos, onClose }: { repos: Schemas["ScmRepository"][]; onClose: () => void }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const [repositoryId, setRepositoryId] = useState(repos[0]?.id ?? "");
  const [name, setName] = useState(issue.suggestedBranch ?? issue.key);
  const [fromRef, setFromRef] = useState("");
  const repo = repos.find((r) => r.id === repositoryId);
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/issues/{issueKey}/branches", {
          params: { path: { issueKey: issue.key } },
          body: { repositoryId, name: name.trim(), fromRef: fromRef.trim() || undefined },
        }),
      ),
    onSuccess: (link) => {
      toast.success(`Branch ${link.externalId} created in ${link.repository.fullPath}`);
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      onClose();
    },
  });
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  const missingKey = name.trim() !== "" && !name.toUpperCase().includes(issue.key.toUpperCase());
  return (
    <Modal
      open
      onClose={onClose}
      title="Create branch"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="create-branch" loading={create.isPending} disabled={!repositoryId || !name.trim() || missingKey}>
            Create branch
          </Button>
        </>
      }
    >
      <form
        id="create-branch"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field label="Repository" error={fe.repositoryId}>
          {(id) => (
            <Select id={id} value={repositoryId} onChange={(e) => setRepositoryId(e.target.value)}>
              {repos.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.fullPath} ({r.provider === "github" ? "GitHub" : "GitLab"})
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field
          label="Branch name"
          error={fe.name ?? (missingKey ? `Include ${issue.key} so commits and pull requests link back to this issue.` : undefined)}
          hint={`Contains ${issue.key} so activity links back here.`}
        >
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} className="font-mono text-[13px]" autoFocus invalid={missingKey || !!fe.name} />}
        </Field>
        <Field label="Branch from" hint={repo ? `Defaults to ${repo.defaultBranch}` : undefined} error={fe.fromRef}>
          {(id) => <Input id={id} value={fromRef} onChange={(e) => setFromRef(e.target.value)} placeholder={repo?.defaultBranch ?? "main"} className="font-mono text-[13px]" />}
        </Field>
        <CopyCommand command={`git fetch origin && git switch ${name.trim() || issue.key}`} />
        {create.error && Object.keys(fe).length === 0 && <ErrorBox error={create.error} />}
      </form>
    </Modal>
  );
}

function CopyCommand({ command }: { command: string }) {
  const toast = useToast();
  return (
    <div className="flex items-center gap-2 rounded-md bg-surface-2 py-1.5 pr-1.5 pl-3">
      <code className="min-w-0 flex-1 truncate font-mono text-xs text-muted">{command}</code>
      <button
        type="button"
        aria-label="Copy command"
        title="Copy command"
        className="rounded p-1 text-faint hover:bg-surface-3 hover:text-ink"
        onClick={() =>
          navigator.clipboard.writeText(command).then(
            () => toast.success("Command copied"),
            () => toast.info(command),
          )
        }
      >
        <Copy className="size-3.5" />
      </button>
    </div>
  );
}
