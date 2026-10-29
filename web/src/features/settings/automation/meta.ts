import type { Schemas } from "@/api/client";

export type Rule = Schemas["AutomationRule"];
export type RuleInput = Schemas["AutomationRuleInput"];
export type Trigger = Schemas["AutomationTrigger"];
export type Action = Schemas["AutomationAction"];
export type TriggerType = Trigger["type"];
export type ActionType = Action["type"];
export type Target = NonNullable<Action["target"]>;
export type Category = Schemas["StatusCategory"];

export const TRIGGERS: { type: TriggerType; label: string; group: "Issues" | "Development" | "Schedule"; hint: string }[] = [
  { type: "issue.created", label: "Issue created", group: "Issues", hint: "When anyone creates an issue in this project." },
  { type: "issue.transitioned", label: "Issue transitioned", group: "Issues", hint: "When an issue enters a status, or any status of a category." },
  { type: "issue.field_changed", label: "Field changed", group: "Issues", hint: "When one of the chosen fields of an issue is edited." },
  { type: "pr.opened", label: "Pull request opened", group: "Development", hint: "A PR / MR mentioning the issue key is opened." },
  { type: "pr.merged", label: "Pull request merged", group: "Development", hint: "A PR / MR mentioning the issue key is merged." },
  { type: "commit.pushed", label: "Commit pushed", group: "Development", hint: "A commit mentioning the issue key is pushed." },
  { type: "branch.created", label: "Branch created", group: "Development", hint: "A branch named after the issue key is created." },
  { type: "pipeline.completed", label: "Pipeline completed", group: "Development", hint: "CI for a branch or commit of the issue succeeds or fails (event.status)." },
  {
    type: "deployment.completed",
    label: "Deployment completed",
    group: "Development",
    hint: "A deployment containing the issue succeeds or fails (event.environment, event.status, event.url).",
  },
  { type: "schedule", label: "On a schedule", group: "Schedule", hint: "Runs the actions for every issue matching a query." },
  { type: "sla.breached", label: "SLA breached", group: "Issues", hint: "Service projects: a request misses an SLA goal (event.metric, event.sla)." },
];

export const ACTIONS: { type: ActionType; label: string }[] = [
  { type: "transition", label: "Transition" },
  { type: "assign", label: "Assign" },
  { type: "set_field", label: "Set a field" },
  { type: "comment", label: "Add a comment" },
  { type: "add_label", label: "Add a label" },
  { type: "rollup_dates", label: "Roll up child dates" },
  { type: "notify", label: "Notify people" },
  { type: "chat", label: "Post to a chat channel" },
  { type: "webhook", label: "Send a webhook" },
];

export const TARGETS: { id: Target; label: string; hint: string }[] = [
  { id: "issue", label: "This issue", hint: "The issue that triggered the rule" },
  { id: "parent", label: "Its parent", hint: "E.g. the epic of a story, the story of a sub-task" },
  { id: "children", label: "Its children", hint: "E.g. the stories of an epic, the sub-tasks of a story" },
];

export const CATEGORIES: { id: Category; label: string }[] = [
  { id: "todo", label: "To do" },
  { id: "in_progress", label: "In progress" },
  { id: "done", label: "Done" },
];

export const categoryName = (c: Category | string | undefined) => CATEGORIES.find((x) => x.id === c)?.label ?? c ?? "";

/** Fields reported by issue.field_changed (custom fields report their key), with readable names. */
export const CHANGEABLE_FIELDS: { key: string; label: string }[] = [
  { key: "summary", label: "summary" },
  { key: "description", label: "description" },
  { key: "type", label: "type" },
  { key: "parent", label: "parent" },
  { key: "priority", label: "priority" },
  { key: "assignee", label: "assignee" },
  { key: "reporter", label: "reporter" },
  { key: "labels", label: "labels" },
  { key: "sprint", label: "sprint" },
  { key: "storyPoints", label: "story points" },
  { key: "startDate", label: "start date" },
  { key: "dueDate", label: "due date" },
  { key: "fixVersions", label: "fix versions" },
];

export const fieldLabel = (key: string) => CHANGEABLE_FIELDS.find((f) => f.key.toLowerCase() === key.toLowerCase())?.label ?? key;

/** splitFields / joinFields convert the trigger's comma-separated field list. */
export function splitFields(field: string | undefined): string[] {
  return (field ?? "")
    .split(",")
    .map((f) => f.trim())
    .filter(Boolean);
}
export const joinFields = (fields: string[]) => fields.join(", ");

export const SET_FIELDS = ["priority", "storyPoints", "summary"];

export const CRON_PRESETS = [
  { label: "Every weekday at 09:00 UTC", cron: "0 9 * * 1-5" },
  { label: "Every Monday at 08:00 UTC", cron: "0 8 * * 1" },
  { label: "Every hour", cron: "0 * * * *" },
  { label: "Daily at midnight UTC", cron: "@daily" },
];

export function triggerLabel(t: TriggerType) {
  return TRIGGERS.find((x) => x.type === t)?.label ?? t;
}

export function emptyAction(type: ActionType, target?: Target): Action {
  const t = target && target !== "issue" ? { target } : {};
  switch (type) {
    case "transition":
      return { type, category: "done", ...t };
    case "assign":
      return { type, assignee: "actor", ...t };
    case "set_field":
      return { type, field: "priority", value: "high", ...t };
    case "comment":
      return { type, body: "", ...t };
    case "add_label":
      return { type, label: "", ...t };
    case "rollup_dates":
      return { type, target: target ?? "parent" };
    case "webhook":
      return { type, url: "https://", ...t };
    case "notify":
      return { type, recipients: "watchers", ...t };
    case "chat":
      return { type, channelId: "", body: "{{issue.key}}: {{issue.summary}}", ...t };
  }
}

/** cleanAction keeps only the keys an action type uses ("issue" is the default target). */
export function cleanAction(a: Action): Action {
  const t = a.target && a.target !== "issue" ? { target: a.target } : {};
  switch (a.type) {
    case "transition":
      return a.category ? { type: a.type, category: a.category, ...t } : { type: a.type, status: a.status, ...t };
    case "assign":
      return { type: a.type, assignee: a.assignee, ...t };
    case "set_field":
      return { type: a.type, field: a.field, value: a.value, ...t };
    case "comment":
      return { type: a.type, body: a.body, ...t };
    case "add_label":
      return { type: a.type, label: a.label, ...t };
    case "rollup_dates":
      return { type: a.type, ...t };
    case "webhook":
      // secret and authValue are write-only: undefined keeps the stored value, "" removes it.
      return { type: a.type, url: a.url, authHeader: a.authHeader, secret: a.secret, authValue: a.authValue, ...t };
    case "notify":
      return { type: a.type, recipients: a.recipients, ...t };
    case "chat":
      return { type: a.type, channelId: a.channelId, body: a.body, ...t };
  }
}

export function cleanTrigger(t: Trigger): Trigger {
  switch (t.type) {
    case "issue.transitioned":
      return t.toCategory ? { type: t.type, toCategory: t.toCategory } : t.toStatus ? { type: t.type, toStatus: t.toStatus } : { type: t.type };
    case "issue.field_changed": {
      const f = joinFields(splitFields(t.field));
      return f ? { type: t.type, field: f } : { type: t.type };
    }
    case "schedule":
      return { type: t.type, cron: t.cron, jql: t.jql };
    case "deployment.completed": {
      const environment = t.environment?.trim();
      return { type: t.type, ...(environment ? { environment } : {}), ...(t.status ? { status: t.status } : {}) };
    }
    default:
      return { type: t.type };
  }
}

/** namedStatuses lists the status names a rule refers to (trigger and transition actions). */
export function namedStatuses(r: Pick<RuleInput, "trigger" | "actions">): string[] {
  const out = new Set<string>();
  if (r.trigger.type === "issue.transitioned" && !r.trigger.toCategory && r.trigger.toStatus) out.add(r.trigger.toStatus);
  for (const a of r.actions) if (a.type === "transition" && !a.category && a.status) out.add(a.status);
  return [...out];
}
