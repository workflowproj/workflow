import { describe, expect, it } from "vitest";
import { describeAction, describeRule, sentence, splitAnd } from "./describe";
import { cleanAction, cleanTrigger, emptyAction, namedStatuses, splitFields, type RuleInput } from "./meta";
import { RULE_TEMPLATES } from "./templates";

const template = (id: string) => RULE_TEMPLATES.find((t) => t.id === id)!.rule;

describe("describeRule", () => {
  it("puts hierarchy rules into words", () => {
    expect(sentence(describeRule(template("subtasks-done")))).toBe(
      "When a sub-task enters Done and its parent has no open children and its parent is not done yet → move parent to Done, comment on parent",
    );
    expect(sentence(describeRule(template("epic-start")))).toBe("When a standard issue enters In progress and its parent is still To do → move parent to In progress");
  });

  it("describes field, status, schedule and development triggers", () => {
    expect(sentence(describeRule(template("epic-dates")))).toBe("When the start date, due date or parent of an issue changes → roll up parent dates from its children");
    expect(sentence(describeRule(template("pr-opened-review")))).toBe("When a pull request is opened for an issue → move to “In Review”");
    expect(describeRule(template("stale-monday")).when).toBe('Every Monday at 08:00 UTC, for issues matching “statusCategory = "In Progress" AND updated <= -14d”');
    expect(sentence(describeRule(template("urgent-bugs")))).toBe("When a bug is created and its priority is highest → notify the watchers, add label “needs-triage”");
    expect(describeRule(template("pipeline-failed")).when).toBe("When a pipeline for an issue completes and it failed");
    expect(sentence(describeRule(template("deployed-production-done")))).toBe("When an issue is deployed to production → move to Done, comment");
    expect(describeRule({ trigger: { type: "deployment.completed", status: "failed" }, condition: "", actions: [{ type: "add_label", label: "x" }] }).when).toBe(
      "When a deployment of an issue fails",
    );
  });

  it("treats a parent guard as implied by parent clauses", () => {
    expect(describeRule({ trigger: { type: "issue.created" }, condition: "has(issue.parent.key)", actions: [{ type: "comment", body: "x" }] }).when).toBe(
      "When an issue is created and it has a parent",
    );
  });

  it("keeps conditions it cannot put into words as code", () => {
    const d = describeRule({ trigger: { type: "issue.created" }, condition: 'issue.labels.exists(l, l == "x") || issue.type == "bug"', actions: [{ type: "comment", body: "hi" }] });
    expect(d.when).toBe("When an issue is created");
    expect(d.condition).toBe('issue.labels.exists(l, l == "x") || issue.type == "bug"');
  });

  it("names action targets", () => {
    expect(describeAction({ type: "add_label", label: "x", target: "children" })).toBe("add label “x” to children");
    expect(describeAction({ type: "assign", assignee: "unassigned", target: "parent" })).toBe("unassign parent");
    expect(describeAction({ type: "notify", recipients: "assignee", target: "parent" })).toBe("notify parent's assignee");
    expect(describeAction({ type: "transition", status: "Blocked" })).toBe("move to “Blocked”");
    expect(describeAction({ type: "chat", channelId: "c", body: "hi" })).toBe("post to chat");
    expect(describeAction({ type: "chat", channelId: "c", body: "hi", target: "parent" }, { channel: () => "#team" })).toBe("post to “#team” about parent");
    expect(describeAction({ type: "chat", channelId: "gone", body: "hi" }, { channel: () => null })).toBe("post to a removed channel");
  });
});

describe("splitAnd", () => {
  it("splits on top-level && only", () => {
    expect(splitAnd('a == "x && y" && b.all(c, c > 1 && c < 3)')).toEqual(['a == "x && y"', "b.all(c, c > 1 && c < 3)"]);
    expect(splitAnd("a || b")).toBeNull();
    expect(splitAnd("(a")).toBeNull();
  });
});

describe("cleaning", () => {
  it("keeps one transition target and drops the default target", () => {
    expect(cleanAction({ type: "transition", status: "Done", category: "done", target: "issue" })).toEqual({ type: "transition", category: "done" });
    expect(cleanAction({ type: "transition", status: "Blocked", target: "children" })).toEqual({ type: "transition", status: "Blocked", target: "children" });
    expect(cleanAction({ type: "rollup_dates", target: "parent", body: "stray" })).toEqual({ type: "rollup_dates", target: "parent" });
    expect(emptyAction("rollup_dates")).toEqual({ type: "rollup_dates", target: "parent" });
  });

  it("normalises triggers", () => {
    expect(cleanTrigger({ type: "issue.transitioned", toStatus: "Done", toCategory: "done" })).toEqual({ type: "issue.transitioned", toCategory: "done" });
    expect(cleanTrigger({ type: "issue.field_changed", field: " startDate ,dueDate,, " })).toEqual({ type: "issue.field_changed", field: "startDate, dueDate" });
    expect(cleanTrigger({ type: "pr.merged", toStatus: "x" })).toEqual({ type: "pr.merged" });
    expect(cleanTrigger({ type: "deployment.completed", environment: " production ", toStatus: "x" })).toEqual({ type: "deployment.completed", environment: "production" });
    expect(cleanTrigger({ type: "deployment.completed", environment: "", status: "failed" })).toEqual({ type: "deployment.completed", status: "failed" });
    expect(splitFields(undefined)).toEqual([]);
  });
});

describe("templates", () => {
  it("are complete rules that survive cleaning unchanged", () => {
    expect(RULE_TEMPLATES.length).toBeGreaterThanOrEqual(8);
    for (const t of RULE_TEMPLATES) {
      const r: RuleInput = t.rule;
      expect(r.name, t.id).toBe(t.title);
      expect(r.actions.length, t.id).toBeGreaterThan(0);
      expect(cleanTrigger(r.trigger), t.id).toEqual(r.trigger);
      expect(r.actions.map(cleanAction), t.id).toEqual(r.actions);
    }
  });

  it("leave the production deployment rule off until it is checked", () => {
    expect(template("deployed-production-done").enabled).toBe(false);
    expect(template("deployed-production-done").trigger).toEqual({ type: "deployment.completed", environment: "production", status: "success" });
  });

  it("report the statuses they name", () => {
    expect(namedStatuses(template("pr-opened-review"))).toEqual(["In Review"]);
    expect(namedStatuses(template("subtasks-done"))).toEqual([]);
  });

  it("remind risk owners of reviews due within a week", () => {
    const t = RULE_TEMPLATES.find((x) => x.id === "risk-reviews")!;
    expect(t.needsRisks).toBe(true);
    expect(t.rule.trigger).toEqual({ type: "schedule", cron: "0 8 * * 1", jql: "kind = risk AND statusCategory != Done AND due <= 7d AND assignee is not EMPTY" });
    expect(t.rule.actions).toEqual([{ type: "notify", recipients: "assignee" }]);
  });
});
