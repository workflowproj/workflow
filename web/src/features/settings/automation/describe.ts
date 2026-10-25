import { categoryName, CRON_PRESETS, fieldLabel, splitFields, type Action, type RuleInput, type Trigger } from "./meta";

/*
 * describeRule turns a rule into one readable sentence for the rules list, e.g.
 *   "When a sub-task enters Done and its parent has no open children → move parent to Done, comment on parent"
 * Conditions written in common shapes become words; anything else is shown as code.
 */

export interface RuleSentence {
  /** "When … [and …]" */
  when: string;
  /** One phrase per action, in order. */
  actions: string[];
  /** The raw CEL condition when it could not be put into words. */
  condition?: string;
}

const levelNoun: Record<string, string> = { "-1": "a sub-task", "0": "a standard issue", "1": "an epic", "2": "an initiative" };

const article = (w: string) => (/^[aeiou]/i.test(w) ? `an ${w}` : `a ${w}`);

function orList(items: string[]): string {
  if (items.length <= 1) return items.join("");
  return `${items.slice(0, -1).join(", ")} or ${items[items.length - 1]}`;
}

/** splitAnd splits a CEL expression on top-level `&&`; it gives up (null) on `||` or unbalanced input. */
export function splitAnd(expr: string): string[] | null {
  const parts: string[] = [];
  let depth = 0;
  let quote = "";
  let start = 0;
  for (let i = 0; i < expr.length; i++) {
    const c = expr[i];
    if (quote) {
      if (c === "\\") i++;
      else if (c === quote) quote = "";
      continue;
    }
    if (c === '"' || c === "'") quote = c;
    else if (c === "(" || c === "[") depth++;
    else if (c === ")" || c === "]") depth--;
    else if (depth === 0 && expr.startsWith("&&", i)) {
      parts.push(expr.slice(start, i).trim());
      start = i + 2;
      i++;
    } else if (depth === 0 && expr.startsWith("||", i)) return null;
  }
  if (depth !== 0 || quote) return null;
  parts.push(expr.slice(start).trim());
  return parts.filter(Boolean);
}

type Clause = { subject?: string; text?: string };

const HAS_PARENT = "it has a parent";

const clausePatterns: [RegExp, (m: RegExpMatchArray) => Clause | null][] = [
  [/^issue\.typeLevel\s*==\s*(-?\d)$/, (m) => (levelNoun[m[1]] ? { subject: levelNoun[m[1]] } : null)],
  [/^issue\.type\s*==\s*"([\w-]+)"$/, (m) => ({ subject: article(m[1].replace(/[_-]/g, " ")) })],
  [/^has\(issue\.parent(\.key)?\)$/, () => ({ text: HAS_PARENT })],
  [/^issue\.parent\.openChildren\s*==\s*0$/, () => ({ text: "its parent has no open children" })],
  [/^issue\.parent\.category\s*==\s*"(todo|in_progress|done)"$/, (m) => ({ text: m[1] === "todo" ? "its parent is still To do" : `its parent is ${categoryName(m[1])}` })],
  [/^issue\.parent\.category\s*!=\s*"(todo|in_progress|done)"$/, (m) => ({ text: m[1] === "done" ? "its parent is not done yet" : `its parent is not ${categoryName(m[1])}` })],
  [/^issue\.priority\s*==\s*"(\w+)"$/, (m) => ({ text: `its priority is ${m[1]}` })],
  [/^issue\.priority\s+in\s+\[([^\]]*)\]$/, (m) => ({ text: `its priority is ${orList(m[1].split(",").map((s) => s.trim().replace(/^"|"$/g, "")))}` })],
  [/^issue\.assignee\s*==\s*""$/, () => ({ text: "it is unassigned" })],
  [/^issue\.statusCategory\s*!=\s*"done"$/, () => ({ text: "it is not done" })],
  [/^event\.status\s*==\s*"failed"$/, () => ({ text: "it failed" })],
  [/^event\.status\s*==\s*"success"$/, () => ({ text: "it succeeded" })],
];

function humanizeCondition(condition: string): { subject?: string; clauses: string[] } | null {
  const parts = splitAnd(condition.trim());
  if (!parts) return null;
  let subject: string | undefined;
  const clauses: string[] = [];
  for (const p of parts) {
    const pattern = clausePatterns.find(([re]) => re.test(p));
    const c = pattern ? pattern[1](p.match(pattern[0])!) : null;
    if (!c) return null;
    if (c.subject) {
      if (subject) return null;
      subject = c.subject;
    }
    if (c.text) clauses.push(c.text);
  }
  // "its parent …" clauses already imply a parent exists.
  const implied = clauses.some((c) => c.startsWith("its parent"));
  return { subject, clauses: implied ? clauses.filter((c) => c !== HAS_PARENT) : clauses };
}

export function cronLabel(cron: string | undefined): string {
  const preset = CRON_PRESETS.find((p) => p.cron === cron?.trim());
  return preset ? preset.label : `On schedule ${cron ?? ""}`.trim();
}

function describeWhen(t: Trigger, s: string): string {
  switch (t.type) {
    case "issue.created":
      return `When ${s} is created`;
    case "issue.transitioned":
      return t.toCategory ? `When ${s} enters ${categoryName(t.toCategory)}` : t.toStatus ? `When ${s} enters “${t.toStatus}”` : `When ${s} changes status`;
    case "issue.field_changed": {
      const fields = splitFields(t.field).map(fieldLabel);
      return fields.length ? `When the ${orList(fields)} of ${s} changes` : `When ${s} is edited`;
    }
    case "pr.opened":
      return `When a pull request is opened for ${s}`;
    case "pr.merged":
      return `When a pull request for ${s} is merged`;
    case "commit.pushed":
      return `When a commit for ${s} is pushed`;
    case "branch.created":
      return `When a branch is created for ${s}`;
    case "pipeline.completed":
      return `When a pipeline for ${s} completes`;
    case "schedule":
      return `${cronLabel(t.cron)}, for issues matching “${t.jql ?? ""}”`;
  }
}

/** describeAction is a short verb phrase for one action, naming its target when it is not the issue itself. */
export function describeAction(a: Action, names: Names = {}): string {
  const obj = a.target === "parent" ? "parent" : a.target === "children" ? "children" : "";
  const sp = obj ? `${obj} ` : "";
  switch (a.type) {
    case "transition":
      return `move ${sp}to ${a.category ? categoryName(a.category) : `“${a.status || "…"}”`}`;
    case "assign":
      if (a.assignee === "unassigned") return `unassign ${obj || "it"}`;
      return `assign ${sp}to ${a.assignee === "actor" ? "whoever triggered it" : a.assignee === "reporter" ? "the reporter" : a.assignee || "…"}`;
    case "set_field":
      return `set ${a.field || "…"}${obj ? ` of ${obj}` : ""} to ${a.value === undefined || a.value === null || a.value === "" ? "…" : String(a.value)}`;
    case "comment":
      return obj ? `comment on ${obj}` : "comment";
    case "add_label":
      return `add label “${a.label || "…"}”${obj ? ` to ${obj}` : ""}`;
    case "rollup_dates":
      return obj === "parent" ? "roll up parent dates from its children" : obj === "children" ? "roll up children's dates from theirs" : "roll up its dates from its children";
    case "notify":
      return `notify ${obj ? `${obj}'s` : "the"} ${a.recipients ?? "watchers"}`;
    case "chat": {
      const ch = a.channelId ? names.channel?.(a.channelId) : undefined;
      return `post to ${ch ? `“${ch}”` : ch === null ? "a removed channel" : "chat"}${obj ? ` about ${obj}` : ""}`;
    }
    case "webhook": {
      let host = "";
      try {
        host = new URL(a.url ?? "").host;
      } catch {
        /* not a URL yet */
      }
      return `send a webhook${host ? ` to ${host}` : ""}`;
    }
  }
}

/** Names resolves ids in actions to display names: undefined when unknown (generic phrase), null when gone. */
export type Names = { channel?: (id: string) => string | null | undefined };

export function describeRule(r: Pick<RuleInput, "trigger" | "condition" | "actions">, names: Names = {}): RuleSentence {
  const cond = r.condition?.trim() ?? "";
  const human = cond ? humanizeCondition(cond) : { clauses: [] };
  const subject = human?.subject ?? "an issue";
  let when = describeWhen(r.trigger, subject);
  if (human?.clauses.length) when += r.trigger.type === "schedule" ? ` where ${human.clauses.join(" and ")}` : ` and ${human.clauses.join(" and ")}`;
  return { when, actions: r.actions.map((a) => describeAction(a, names)), condition: cond && !human ? cond : undefined };
}

/** sentence joins a description into plain text (for titles and tests). */
export function sentence(d: RuleSentence): string {
  return `${d.when}${d.condition ? ` if ${d.condition}` : ""} → ${d.actions.join(", ")}`;
}
