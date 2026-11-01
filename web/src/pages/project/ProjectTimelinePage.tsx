import { useCallback } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { Timeline } from "@/features/timeline/Timeline";
import { useProject } from "@/api/queries";
import { isProgramme } from "@/lib/project";

const route = getRouteApi("/app/projects/$projectKey/timeline");

/** ProjectTimelinePage is one project's timeline (route "/app/projects/$projectKey/timeline"). */
export function ProjectTimelinePage() {
  const { projectKey } = route.useParams();
  const { jql, issue } = route.useSearch();
  const navigate = route.useNavigate();
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  const { data: project } = useProject(projectKey);
  // ProjectLayout already keeps this project's live updates open; a programme's timeline also
  // follows its teams.
  return (
    <>
      {!project ? null : isProgramme(project) ? (
        <Timeline projects="" programme={projectKey} multi baseJql={jql} onOpen={openIssue} live />
      ) : (
        <Timeline projects={projectKey} multi={false} baseJql={jql} onOpen={openIssue} />
      )}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </>
  );
}
