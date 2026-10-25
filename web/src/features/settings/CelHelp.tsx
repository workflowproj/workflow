import { BookOpen, ChevronRight } from "lucide-react";
import { cn } from "@/lib/cn";

export type CelExample = { label: string; expr: string };

type Var = { name: string; desc: string };

const issueVars: Var[] = [
  { name: "issue.key / summary / type / priority", desc: 'strings, e.g. issue.type == "bug"' },
  { name: "issue.typeLevel", desc: "1 epic, 0 story / task / bug, -1 sub-task" },
  { name: "issue.status / statusCategory", desc: "current status name and its category (todo, in_progress, done)" },
  { name: "issue.assignee / reporter", desc: '"" when unset, otherwise an email address' },
  { name: "issue.labels / storyPoints / resolution", desc: "list of strings, number, string" },
  { name: "issue.startDate / dueDate", desc: '"YYYY-MM-DD", "" when unset' },
  { name: "issue.fields", desc: "custom fields by key: issue.fields.team" },
  { name: "issue.subtasks / children", desc: "[{key, summary, type, status, category}]" },
  { name: "issue.parent", desc: "{key, type, status, category, childCount, openChildren, startDate, dueDate}; {} without a parent, so guard with has(issue.parent.key)" },
  { name: "issue.parent.openChildren", desc: "children of the parent not done, this issue included: 0 = all done" },
  { name: "issue.devLinks", desc: "[{kind: branch|commit|pr|pipeline, state, title, merged}]" },
  { name: "issue.links", desc: "[{type, direction, key, category}]" },
];
const actorVars: Var[] = [
  { name: "actor.email / name / orgRole", desc: "who runs the transition" },
  { name: 'actor.hasRole("admin")', desc: "project role check" },
  { name: 'actor.hasPermission("issue.delete")', desc: "permission check" },
  { name: "actor.type", desc: "user, automation or scm" },
];
const transitionVars: Var[] = [{ name: "transition.id / name / from / to", desc: "the transition being run" }];
const eventVars: Var[] = [
  { name: "event.type / actorType", desc: 'e.g. "issue.transitioned", "user"' },
  { name: "event.from / to / fromCategory / toCategory", desc: "issue.transitioned: status names and categories" },
  { name: "event.transition", desc: "issue.transitioned: the transition's name" },
  { name: "event.fields", desc: "issue.field_changed: the changed field names" },
  { name: "event.title / url / state / number / branch", desc: "pull requests" },
  { name: "event.status / pipeline / url / branch", desc: 'pipelines; status is "success" or "failed"' },
  { name: "event.sha / message / branch", desc: "commits" },
];

/**
 * CelHelp is a collapsible reference of the variables available to CEL expressions, with
 * examples that can be inserted. `context` picks transition or automation variables.
 */
export function CelHelp({ context, examples, onInsert, className }: { context: "transition" | "automation"; examples?: CelExample[]; onInsert?: (expr: string) => void; className?: string }) {
  const groups: [string, Var[]][] =
    context === "transition"
      ? [
          ["issue", issueVars],
          ["actor", actorVars],
          ["transition", transitionVars],
        ]
      : [
          ["issue", issueVars],
          ["event", eventVars],
        ];
  return (
    <details className={cn("group rounded-md border border-line bg-surface-2/60 text-[13px]", className)}>
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-2.5 py-1.5 text-muted select-none hover:text-ink [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3.5 transition-transform group-open:rotate-90" />
        <BookOpen className="size-3.5" />
        CEL reference{examples?.length ? " and examples" : ""}
      </summary>
      <div className="flex flex-col gap-3 border-t border-line px-3 py-2.5">
        {groups.map(([title, vars]) => (
          <div key={title}>
            <p className="mb-1 text-[11px] font-semibold tracking-wide text-faint uppercase">{title}</p>
            <ul className="flex flex-col gap-0.5">
              {vars.map((v) => (
                <li key={v.name} className="leading-snug">
                  <code className="font-mono text-[12px] text-ink">{v.name}</code> <span className="text-muted">— {v.desc}</span>
                </li>
              ))}
            </ul>
          </div>
        ))}
        <p className="text-muted">
          Also <code className="font-mono text-[12px]">now</code> (timestamp) and the CEL string, list and math extensions, e.g.{" "}
          <code className="font-mono text-[12px]">issue.labels.exists(l, l == "urgent")</code>. Expressions must return a boolean.
        </p>
        {examples && examples.length > 0 && (
          <div>
            <p className="mb-1 text-[11px] font-semibold tracking-wide text-faint uppercase">Examples</p>
            <div className="flex flex-col gap-1">
              {examples.map((e) => (
                <button
                  key={e.expr}
                  type="button"
                  disabled={!onInsert}
                  onClick={() => onInsert?.(e.expr)}
                  className="flex flex-col items-start rounded px-2 py-1 text-left hover:bg-surface-3 disabled:hover:bg-transparent"
                  title={onInsert ? "Use this expression" : undefined}
                >
                  <span className="text-ink">{e.label}</span>
                  <code className="font-mono text-[12px] text-muted">{e.expr}</code>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    </details>
  );
}
