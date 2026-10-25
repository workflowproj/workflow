import { useEffect } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { IssueView } from "@/features/issues/IssueView";
import { issueQuery } from "@/features/issues/issue-api";
import { useProjectEvents } from "@/lib/events";

const route = getRouteApi("/app/browse/$issueKey");

/** IssuePage shows one issue full-page and keeps it live through the project's event stream. */
export function IssuePage() {
  const { issueKey } = route.useParams();
  const navigate = useNavigate();
  const { data: issue } = useQuery(issueQuery(issueKey));
  useProjectEvents(issue?.projectKey);

  // Old keys (renamed projects) and lower-case keys land on the canonical URL.
  const canonical = issue?.key;
  useEffect(() => {
    if (canonical && canonical !== issueKey) navigate({ to: "/browse/$issueKey", params: { issueKey: canonical }, replace: true });
  }, [canonical, issueKey, navigate]);

  return (
    <div className="mx-auto w-full max-w-[1320px]">
      <IssueView issueKey={issueKey} variant="page" />
    </div>
  );
}
