import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useProject } from "@/api/queries";
import { IssueNavigator, type NavigatorSearch } from "@/features/search/IssueNavigator";

const route = getRouteApi("/app/projects/$projectKey/issues");

/** ProjectIssuesPage is the issue navigator scoped to one project (live updates come from ProjectLayout). */
export function ProjectIssuesPage() {
  const { projectKey } = route.useParams();
  const search = route.useSearch();
  const navigate = useNavigate();
  const { data: project } = useProject(projectKey);
  const key = project?.key ?? projectKey.toUpperCase();
  return (
    <IssueNavigator
      key={key}
      projectKey={key}
      title="Issues"
      search={search}
      onSearch={(next: NavigatorSearch) => navigate({ to: "/projects/$projectKey/issues", params: { projectKey }, search: { jql: next.jql, issue: next.issue } })}
    />
  );
}
