import { CalendarRange, CircleCheckBig, GitMerge, GitPullRequestArrow, ListChecks, Megaphone, OctagonAlert, Play, Rocket, Siren, Timer, type LucideIcon } from "lucide-react";
import type { RuleInput } from "./meta";

export interface RuleTemplate {
  id: string;
  title: string;
  description: string;
  group: "Development" | "Hierarchy" | "Hygiene" | "Chat";
  icon: LucideIcon;
  rule: RuleInput;
  /** Offered only when the project has a chat channel; the first one is pre-selected. */
  needsChat?: boolean;
}

/*
 * Ready-made rules. Each opens the rule dialog pre-filled so it can be reviewed (and adapted to
 * the project's statuses) before it is created. Named statuses are checked against the
 * project's workflows in the dialog.
 */
export const RULE_TEMPLATES: RuleTemplate[] = [
  {
    id: "pr-opened-review",
    title: "Move to In Review when a pull request opens",
    description: "Opening a PR or MR that mentions the issue key moves the issue to review.",
    group: "Development",
    icon: GitPullRequestArrow,
    rule: {
      name: "Move to In Review when a pull request opens",
      description: "Opening a pull request that mentions the issue moves it to In Review.",
      enabled: true,
      trigger: { type: "pr.opened" },
      condition: "",
      actions: [{ type: "transition", status: "In Review" }],
    },
  },
  {
    id: "pr-merged-done",
    title: "Close issues when their pull request merges",
    description: "Merging the PR moves the issue to its workflow's Done status and links the merge.",
    group: "Development",
    icon: GitMerge,
    rule: {
      name: "Close issues when their pull request merges",
      description: "Merging a pull request that mentions the issue finishes it.",
      enabled: true,
      trigger: { type: "pr.merged" },
      condition: "",
      actions: [
        { type: "transition", category: "done" },
        { type: "comment", body: "Merged in {{event.url}}" },
      ],
    },
  },
  {
    id: "epic-start",
    title: "Start the epic when work starts on one of its stories",
    description: "The first story to go In progress moves its epic to In progress too.",
    group: "Hierarchy",
    icon: Play,
    rule: {
      name: "Start the epic when work starts on one of its stories",
      description: "Keeps epics in step with the work inside them.",
      enabled: true,
      trigger: { type: "issue.transitioned", toCategory: "in_progress" },
      condition: 'issue.typeLevel == 0 && has(issue.parent.key) && issue.parent.category == "todo"',
      actions: [{ type: "transition", target: "parent", category: "in_progress" }],
    },
  },
  {
    id: "subtasks-done",
    title: "Finish the parent when its last sub-task is done",
    description: "When every sub-task is done, the story moves to Done with a note on which one was last.",
    group: "Hierarchy",
    icon: ListChecks,
    rule: {
      name: "Finish the parent when its last sub-task is done",
      description: "Closes stories whose sub-tasks are all done.",
      enabled: true,
      trigger: { type: "issue.transitioned", toCategory: "done" },
      condition: 'issue.typeLevel == -1 && has(issue.parent.key) && issue.parent.openChildren == 0 && issue.parent.category != "done"',
      actions: [
        { type: "transition", target: "parent", category: "done" },
        { type: "comment", target: "parent", body: "All sub-tasks done ({{trigger.key}} was last)." },
      ],
    },
  },
  {
    id: "stories-done",
    title: "Close the epic when its last story is done",
    description: "When every story, task and bug in an epic is done, the epic moves to Done.",
    group: "Hierarchy",
    icon: CircleCheckBig,
    rule: {
      name: "Close the epic when its last story is done",
      description: "Closes epics whose issues are all done.",
      enabled: true,
      trigger: { type: "issue.transitioned", toCategory: "done" },
      condition: 'issue.typeLevel == 0 && has(issue.parent.key) && issue.parent.openChildren == 0 && issue.parent.category != "done"',
      actions: [
        { type: "transition", target: "parent", category: "done" },
        { type: "comment", target: "parent", body: "All issues in this epic are done ({{trigger.key}} was last)." },
      ],
    },
  },
  {
    id: "epic-dates",
    title: "Keep epic dates covering their stories",
    description: "When a story's dates or epic change, the epic's start and due dates stretch to span its stories — so the timeline stays right.",
    group: "Hierarchy",
    icon: CalendarRange,
    rule: {
      name: "Keep epic dates covering their stories",
      description: "Rolls story dates up to their epic for the timeline.",
      enabled: true,
      trigger: { type: "issue.field_changed", field: "startDate, dueDate, parent" },
      condition: "",
      actions: [{ type: "rollup_dates", target: "parent" }],
    },
  },
  {
    id: "stale-monday",
    title: "Flag stale work every Monday",
    description: "Labels in-progress issues that have not been updated for two weeks and tells their assignees.",
    group: "Hygiene",
    icon: Timer,
    rule: {
      name: "Flag stale work every Monday",
      description: "Surfaces in-progress issues nobody has touched for two weeks.",
      enabled: true,
      trigger: { type: "schedule", cron: "0 8 * * 1", jql: 'statusCategory = "In Progress" AND updated <= -14d' },
      condition: "",
      actions: [
        { type: "add_label", label: "stale" },
        { type: "notify", recipients: "assignee" },
      ],
    },
  },
  {
    id: "urgent-bugs",
    title: "Triage new urgent bugs",
    description: "A new bug with the highest priority alerts its watchers and is labelled for triage.",
    group: "Hygiene",
    icon: Siren,
    rule: {
      name: "Triage new urgent bugs",
      description: "Makes sure urgent bugs are seen straight away.",
      enabled: true,
      trigger: { type: "issue.created" },
      condition: 'issue.type == "bug" && issue.priority == "highest"',
      actions: [
        { type: "notify", recipients: "watchers" },
        { type: "add_label", label: "needs-triage" },
      ],
    },
  },
  {
    id: "deployed-production-done",
    title: "Move issues to Done when they reach production",
    description: "A successful deployment to production that contains the issue finishes it. Created turned off: check the environment's name first.",
    group: "Development",
    icon: Rocket,
    rule: {
      name: "Move issues to Done when they reach production",
      description: "Issues are done once they are live.",
      enabled: false,
      trigger: { type: "deployment.completed", environment: "production", status: "success" },
      condition: "",
      actions: [
        { type: "transition", category: "done" },
        { type: "comment", body: "Deployed to {{event.environment}}: {{event.url}}" },
      ],
    },
  },
  {
    id: "pipeline-failed",
    title: "Flag issues whose pipeline fails",
    description: "A failed CI run for the issue's branch or commit adds a “ci-failing” label and a comment with the link. Remove the label once fixed.",
    group: "Development",
    icon: OctagonAlert,
    rule: {
      name: "Flag issues whose pipeline fails",
      description: "Makes failing builds visible on the board.",
      enabled: true,
      trigger: { type: "pipeline.completed" },
      condition: 'event.status == "failed"',
      actions: [
        { type: "add_label", label: "ci-failing" },
        { type: "comment", body: "Pipeline {{event.pipeline}} failed on {{event.branch}}: {{event.url}}" },
      ],
    },
  },
  {
    id: "chat-done",
    title: "Announce finished work in chat",
    description: "Posts to a Slack or Mattermost channel whenever an issue reaches a done status.",
    group: "Chat",
    icon: Megaphone,
    needsChat: true,
    rule: {
      name: "Announce finished work in chat",
      description: "Celebrates finished work in the team channel.",
      enabled: true,
      trigger: { type: "issue.transitioned", toCategory: "done" },
      condition: "",
      actions: [{ type: "chat", channelId: "", body: "🎉 {{issue.key}} {{issue.summary}} is done" }],
    },
  },
];
