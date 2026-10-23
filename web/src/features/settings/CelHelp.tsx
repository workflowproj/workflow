import { BookOpen, ChevronRight } from "lucide-react";
import { cn } from "@/lib/cn";

export type CelExample = { label: string; expr: string };

type Var = { name: string; desc: string };

const issueVars: Var[] = [
  { name: "issue.key / summary / type / priority", desc: 'strings, e.g. issue.type == "bug"' },
  { name: "issue.status / statusCategory", desc: "current status name and its category (todo, in_progress, done)" },
  { name: "issue.assignee / reporter", desc: 'email addresses, "" when unset' },
  { name: "issue.labels", desc: "list of strings" },
  { name: "issue.storyPoints / resolution / fields", desc: "custom fields by key: issue.fields.team" },
  { name: "issue.subtasks / children", desc: "[{key, summary, type, status, category}]" },
  { name: "issue.devLinks", desc: "[{kind: branch|commit|pr|pipeline, state, title, merged}]" },
  { name: "issue.links / parent", desc: "[{type, direction, key, category}] and {key, type, status, category}" },
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
  { name: "event.from / to / transition", desc: "issue.transitioned: status names" },
  { name: "event.fields", desc: "issue.field_changed: changed field names" },
  { name: "event.branch / title / url / state / number", desc: "development events (branch, PR, commit, pipeline)" },
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
