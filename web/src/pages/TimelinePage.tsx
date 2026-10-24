import { useCallback } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { Timeline } from "@/features/timeline/Timeline";
import { ProjectPicker } from "@/features/timeline/Toolbar";

const route = getRouteApi("/app/timeline");

/** TimelinePage is the big-picture timeline across projects (route "/app/timeline"). */
export function TimelinePage() {
  const { projects, jql, issue } = route.useSearch();
  const navigate = route.useNavigate();
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  const keys = projects ? projects.split(",").filter(Boolean) : [];
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="shrink-0 border-b border-line bg-surface px-4 pt-4 pb-3 md:px-6">
        <h1 className="text-lg leading-tight font-semibold">Timeline</h1>
        <p className="text-xs text-muted">The big picture: epics, issues, sprints, releases and the dependencies between them, across projects.</p>
      </div>
      <div className="min-h-0 flex-1">
        <Timeline
          projects={keys.join(",")}
          multi
          baseJql={jql}
          onOpen={openIssue}
          live
          picker={<ProjectPicker value={keys} onChange={(k) => navigate({ search: (s) => ({ ...s, projects: k.length ? k.join(",") : undefined }) })} />}
        />
      </div>
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}
