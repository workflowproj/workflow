import type { Schemas } from "@/api/client";

export type Rule = Schemas["AutomationRule"];
export type RuleInput = Schemas["AutomationRuleInput"];
export type Trigger = Schemas["AutomationTrigger"];
export type Action = Schemas["AutomationAction"];
export type TriggerType = Trigger["type"];
export type ActionType = Action["type"];

export const TRIGGERS: { type: TriggerType; label: string; group: "Issues" | "Development" | "Schedule"; hint: string }[] = [
  { type: "issue.created", label: "Issue created", group: "Issues", hint: "When anyone creates an issue in this project." },
  { type: "issue.transitioned", label: "Issue transitioned", group: "Issues", hint: "When an issue moves to another status." },
  { type: "issue.field_changed", label: "Field changed", group: "Issues", hint: "When a field of an issue is edited." },
  { type: "pr.opened", label: "Pull request opened", group: "Development", hint: "A PR / MR mentioning the issue key is opened." },
  { type: "pr.merged", label: "Pull request merged", group: "Development", hint: "A PR / MR mentioning the issue key is merged." },
  { type: "commit.pushed", label: "Commit pushed", group: "Development", hint: "A commit mentioning the issue key is pushed." },
  { type: "branch.created", label: "Branch created", group: "Development", hint: "A branch named after the issue key is created." },
  { type: "pipeline.completed", label: "Pipeline completed", group: "Development", hint: "CI for a branch or commit of the issue succeeds or fails." },
  { type: "schedule", label: "On a schedule", group: "Schedule", hint: "Runs the actions for every issue matching a JQL query." },
];

export const ACTIONS: { type: ActionType; label: string }[] = [
  { type: "transition", label: "Transition the issue" },
  { type: "assign", label: "Assign the issue" },
  { type: "set_field", label: "Set a field" },
  { type: "comment", label: "Add a comment" },
  { type: "add_label", label: "Add a label" },
  { type: "webhook", label: "Send a webhook" },
  { type: "notify", label: "Notify people" },
];

/** Fields reported by issue.field_changed (plus custom field keys). */
export const CHANGEABLE_FIELDS = ["summary", "description", "priority", "assignee", "reporter", "labels", "storyPoints", "startDate", "dueDate", "sprint", "parent", "fixVersions"];

export const SET_FIELDS = ["priority", "storyPoints", "summary"];

export const CRON_PRESETS = [
  { label: "Every weekday at 09:00 UTC", cron: "0 9 * * 1-5" },
  { label: "Every hour", cron: "0 * * * *" },
  { label: "Every Monday at 08:00 UTC", cron: "0 8 * * 1" },
  { label: "Daily at midnight UTC", cron: "@daily" },
];

export function triggerLabel(t: TriggerType) {
  return TRIGGERS.find((x) => x.type === t)?.label ?? t;
}

/** describeTrigger is a one-line, human summary of a rule's trigger. */
export function describeTrigger(t: Trigger): string {
  switch (t.type) {
    case "issue.transitioned":
      return t.toStatus ? `When an issue moves to ${t.toStatus}` : "When an issue changes status";
    case "issue.field_changed":
      return t.field ? `When ${t.field} changes` : "When any field changes";
    case "schedule":
      return `On schedule ${t.cron ?? ""} for ${t.jql ? `“${t.jql}”` : "matching issues"}`;
    default:
      return `When: ${triggerLabel(t.type).toLowerCase()}`;
  }
}

export function describeAction(a: Action): string {
  switch (a.type) {
    case "transition":
      return `Move to ${a.status || "…"}`;
    case "assign":
      return a.assignee === "unassigned" ? "Unassign" : `Assign to ${a.assignee === "actor" ? "the person who triggered it" : a.assignee || "…"}`;
    case "set_field":
      return `Set ${a.field || "…"} to ${a.value === undefined || a.value === "" ? "…" : String(a.value)}`;
    case "comment":
      return "Comment";
    case "add_label":
      return `Add label ${a.label || "…"}`;
    case "webhook": {
      let host = "";
      try {
        host = new URL(a.url ?? "").host;
      } catch {
        /* not a URL yet */
      }
      return `POST to ${host || "…"}`;
    }
    case "notify":
      return `Notify the ${a.recipients ?? "watchers"}`;
  }
}

export function emptyAction(type: ActionType): Action {
  switch (type) {
    case "transition":
      return { type, status: "" };
    case "assign":
      return { type, assignee: "actor" };
    case "set_field":
      return { type, field: "priority", value: "high" };
    case "comment":
      return { type, body: "" };
    case "add_label":
      return { type, label: "" };
    case "webhook":
      return { type, url: "https://" };
    case "notify":
      return { type, recipients: "watchers" };
  }
}

/** cleanAction keeps only the keys an action type uses. */
export function cleanAction(a: Action): Action {
  switch (a.type) {
    case "transition":
      return { type: a.type, status: a.status };
    case "assign":
      return { type: a.type, assignee: a.assignee };
    case "set_field":
      return { type: a.type, field: a.field, value: a.value };
    case "comment":
      return { type: a.type, body: a.body };
    case "add_label":
      return { type: a.type, label: a.label };
    case "webhook":
      return { type: a.type, url: a.url };
    case "notify":
      return { type: a.type, recipients: a.recipients };
  }
}

export function cleanTrigger(t: Trigger): Trigger {
  switch (t.type) {
    case "issue.transitioned":
      return t.toStatus ? { type: t.type, toStatus: t.toStatus } : { type: t.type };
    case "issue.field_changed":
      return t.field ? { type: t.type, field: t.field } : { type: t.type };
    case "schedule":
      return { type: t.type, cron: t.cron, jql: t.jql };
    default:
      return { type: t.type };
  }
}
