import { Hash, MessagesSquare, type LucideIcon } from "lucide-react";
import { queryOptions } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

export type ChatChannel = Schemas["ChatChannel"];
export type ChatProvider = Schemas["ChatProvider"];
export type ChatEvent = Schemas["ChatEvent"];
export type ChatDelivery = Schemas["ChatDelivery"];

export const PROVIDERS: { id: ChatProvider; label: string; icon: LucideIcon; urlExample: string; steps: string[] }[] = [
  {
    id: "slack",
    label: "Slack",
    icon: Hash,
    urlExample: "https://hooks.slack.com/services/T…/B…/…",
    steps: ["Create (or reuse) a Slack app for your workspace.", "Open Incoming Webhooks, turn them on, then Add New Webhook to Workspace.", "Pick the channel and copy the webhook URL (https://hooks.slack.com/services/…)."],
  },
  {
    id: "mattermost",
    label: "Mattermost",
    icon: MessagesSquare,
    urlExample: "https://mattermost.example.com/hooks/…",
    steps: ["In Mattermost, open Integrations → Incoming Webhooks → Add Incoming Webhook.", "Choose the channel, save, and copy the URL (https://your-mattermost/hooks/…)."],
  },
];

export const providerOf = (id: ChatProvider) => PROVIDERS.find((p) => p.id === id) ?? PROVIDERS[0];

export const EVENT_GROUPS: { title: string; events: { id: Exclude<ChatEvent, "*">; label: string }[] }[] = [
  {
    title: "Issues",
    events: [
      { id: "issue.created", label: "Issue created" },
      { id: "issue.transitioned", label: "Status changed" },
      { id: "issue.assigned", label: "Assignee changed" },
      { id: "comment.created", label: "Comment added" },
    ],
  },
  {
    title: "Planning",
    events: [
      { id: "sprint.started", label: "Sprint started" },
      { id: "sprint.completed", label: "Sprint completed" },
      { id: "release.released", label: "Release shipped" },
    ],
  },
  {
    title: "Development",
    events: [
      { id: "pr.opened", label: "Pull request opened" },
      { id: "pr.merged", label: "Pull request merged" },
      { id: "pipeline.failed", label: "Pipeline failed" },
    ],
  },
];

export const eventLabel = (e: string) => EVENT_GROUPS.flatMap((g) => g.events).find((x) => x.id === e)?.label ?? e;

export const chatChannelsQuery = (projectKey: string) =>
  queryOptions({
    queryKey: ["chat-channels", projectKey],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/chat-channels", { params: { path: { projectKey } } })),
    staleTime: 30_000,
  });

export const chatDeliveriesQuery = (channelId: string, limit: number) =>
  queryOptions({
    queryKey: ["chat-deliveries", channelId, limit],
    queryFn: () => unwrap(api.GET("/chat-channels/{channelId}/deliveries", { params: { path: { channelId }, query: { limit } } })),
    refetchInterval: (q) => (q.state.data?.some((d) => d.status === "pending") ? 2000 : 20_000),
  });
