import { getRouteApi } from "@tanstack/react-router";
import { WebhooksManager } from "@/features/settings/webhooks/WebhooksManager";

const route = getRouteApi("/app/projects/$projectKey/settings/webhooks");

export function WebhooksSettings() {
  const { projectKey } = route.useParams();
  return <WebhooksManager projectKey={projectKey} />;
}
