import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { IssueNavigator, type NavigatorSearch } from "@/features/search/IssueNavigator";
import { issueQuery } from "@/features/issues/issue-api";
import { useProjectEvents } from "@/lib/events";

const route = getRouteApi("/app/issues");

/** SearchPage is the global issue navigator (/issues?jql=…&filter=…&issue=…). */
export function SearchPage() {
  const search = route.useSearch();
  const navigate = useNavigate();
  // Keep the issue open in the drawer live, like project pages do.
  const { data: open } = useQuery({ ...issueQuery(search.issue ?? ""), enabled: !!search.issue });
  useProjectEvents(open?.projectKey);
  return <IssueNavigator search={search} onSearch={(next: NavigatorSearch) => navigate({ to: "/issues", search: next })} />;
}
