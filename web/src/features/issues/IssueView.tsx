import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FileQuestion, Link2, ListPlus, Paperclip, TriangleAlert } from "lucide-react";
import { ApiError } from "@/api/client";
import { hasPermission, issueTypesQuery, useProject } from "@/api/queries";
import { Button, EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { issueQuery } from "./issue-api";
import { IssueProvider, useIssue } from "./issue-context";
import { IssueTopBar, SummaryField } from "./IssueHeader";
import { StatusControl } from "./StatusControl";
import { TransitionProblem } from "./TransitionProblem";
import { DescriptionSection } from "./DescriptionSection";
import { AttachmentsSection, DropOverlay, useAttachmentUpload, useFileDrop } from "./AttachmentsSection";
import { ChildrenSection } from "./ChildrenSection";
import { LinksSection } from "./LinksSection";
import { DevelopmentPanel } from "./DevelopmentPanel";
import { ActivitySection } from "./ActivitySection";
import { DetailsPanel } from "./DetailsPanel";
import { MetricsPanel } from "./MetricsPanel";

/**
 * IssueView is the full issue view: header, description, sub-tasks, links, comments,
 * attachments, history, development panel and the sidebar (details and metrics). Used by the /browse page
 * and inside the board/backlog/search drawer (`variant="drawer"`, with `onClose`).
 * `onNavigate` lets a drawer switch to another issue (e.g. clicking a sub-task) in place.
 */
export function IssueView({
  issueKey,
  variant = "page",
  onClose,
  onNavigate,
}: {
  issueKey: string;
  variant?: "page" | "drawer";
  onClose?: () => void;
  onNavigate?: (key: string) => void;
}) {
  const { data: issue, isLoading, error } = useQuery(issueQuery(issueKey));
  if (isLoading) return <PageSpinner label={`Loading ${issueKey}`} />;
  if (!issue) return <IssueUnavailable issueKey={issueKey} error={error} onClose={onClose} />;
  return (
    <IssueProvider issue={issue} variant={variant} onNavigate={onNavigate} onClose={onClose}>
      <IssueLayout key={issue.key} gone={!!error} />
    </IssueProvider>
  );
}

function IssueLayout({ gone }: { gone: boolean }) {
  const { issue } = useIssue();
  const [problem, setProblem] = useState<unknown>(null);
  const { upload, uploading } = useAttachmentUpload(issue.key);
  const { over, handlers } = useFileDrop(issue.permissions.comment, upload);
  return (
    <div className="@container/issue relative min-h-full" {...handlers}>
      <IssueTopBar />
      {gone && (
        <div role="alert" className="mx-4 mb-2 flex items-center gap-2 rounded-md border border-warning/40 bg-warning-soft px-3 py-2 text-sm text-warning sm:mx-6">
          <TriangleAlert className="size-4 shrink-0" />
          This issue could not be refreshed — it may have been deleted or moved out of your reach.
        </div>
      )}
      <div
        className={
          "grid gap-x-8 gap-y-6 px-4 pt-3 pb-16 sm:px-6 [grid-template-areas:'head'_'side'_'body'] " +
          "@4xl/issue:grid-cols-[minmax(0,1fr)_20rem] @4xl/issue:grid-rows-[auto_1fr] @4xl/issue:[grid-template-areas:'head_side'_'body_side']"
        }
      >
        <div className="flex min-w-0 flex-col gap-3 [grid-area:head]">
          <SummaryField />
          <div className="flex flex-wrap items-center gap-2">
            <StatusControl onProblem={setProblem} />
            <QuickActions />
          </div>
          {!!problem && <TransitionProblem error={problem} onDismiss={() => setProblem(null)} />}
        </div>
        <aside className="flex min-w-0 flex-col gap-4 [grid-area:side]">
          <DetailsPanel />
          <MetricsPanel />
        </aside>
        <div className="flex min-w-0 flex-col gap-8 [grid-area:body]">
          <DescriptionSection />
          <AttachmentsSection upload={upload} uploading={uploading} />
          <ChildrenSection />
          <LinksSection />
          <DevelopmentPanel />
          <ActivitySection />
        </div>
      </div>
      {over && <DropOverlay issueKey={issue.key} />}
    </div>
  );
}

/** QuickActions are the shortcuts next to the status button. */
function QuickActions() {
  const { issue, request } = useIssue();
  const { data: types = [] } = useQuery(issueTypesQuery(issue.projectKey));
  const { data: project } = useProject(issue.projectKey);
  const p = issue.permissions;
  const childLevel = issue.type.level >= 0 && hasPermission(project, "issue.create") && types.some((t) => t.level === issue.type.level - 1);
  return (
    <>
      {p.comment && (
        <Button size="sm" variant="subtle" icon={<Paperclip className="size-3.5" />} onClick={() => request("attach")} title="Attach files (or drop them anywhere on the issue)">
          Attach
        </Button>
      )}
      {childLevel && (
        <Button size="sm" variant="subtle" icon={<ListPlus className="size-3.5" />} onClick={() => request("add-child")}>
          {issue.type.level === 0 ? "Add sub-task" : "Add child issue"}
        </Button>
      )}
      {p.edit && (
        <Button size="sm" variant="subtle" icon={<Link2 className="size-3.5" />} onClick={() => request("add-link")}>
          Link issue
        </Button>
      )}
    </>
  );
}

function IssueUnavailable({ issueKey, error, onClose }: { issueKey: string; error: unknown; onClose?: () => void }) {
  const notFound = error instanceof ApiError && (error.status === 404 || error.status === 403);
  return (
    <div className="p-6">
      {notFound || !error ? (
        <EmptyState
          icon={<FileQuestion className="size-8" />}
          title={`${issueKey} is not available`}
          body="The issue does not exist, was deleted, or you do not have access to its project."
          action={onClose && <Button onClick={onClose}>Close</Button>}
        />
      ) : (
        <ErrorBox error={error} />
      )}
    </div>
  );
}
