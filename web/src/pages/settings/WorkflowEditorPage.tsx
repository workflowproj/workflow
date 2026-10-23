import { getRouteApi } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { workflowQuery } from "@/features/settings/queries";
import { WorkflowEditor } from "@/features/workflow-editor/WorkflowEditor";

const route = getRouteApi("/app/projects/$projectKey/settings/workflows/$workflowId");

export function WorkflowEditorPage() {
  const { projectKey, workflowId } = route.useParams();
  // Poll while a published version is still migrating issues in the background.
  const { data, isLoading, error } = useQuery({ ...workflowQuery(workflowId), refetchInterval: (q) => (q.state.data?.pendingMigration ? 2500 : false) });
  if (isLoading) return <PageSpinner label="Loading workflow" />;
  if (error || !data) return <ErrorBox error={error ?? new Error("Workflow not found")} />;
  return <WorkflowEditor key={data.id} workflow={data} projectKey={projectKey} />;
}
