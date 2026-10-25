import { useCallback } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { Timeline } from "@/features/timeline/Timeline";

const route = getRouteApi("/app/projects/$projectKey/timeline");

/** ProjectTimelinePage is one project's timeline (route "/app/projects/$projectKey/timeline"). */
export function ProjectTimelinePage() {
  const { projectKey } = route.useParams();
  const { jql, issue } = route.useSearch();
  const navigate = route.useNavigate();
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  // ProjectLayout already keeps this project's live updates open.
  return (
    <>
      <Timeline projects={projectKey} multi={false} baseJql={jql} onOpen={openIssue} />
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </>
  );
}
