import { Check, TriangleAlert } from "lucide-react";
import { Badge } from "@/components/ui";
import { cn } from "@/lib/cn";
import { describeRule, sentence } from "./describe";
import { namedStatuses } from "./meta";
import { RULE_TEMPLATES, type RuleTemplate } from "./templates";

const GROUPS: { id: RuleTemplate["group"]; title: string }[] = [
  { id: "Development", title: "Pull requests and builds" },
  { id: "Hierarchy", title: "Epics, stories and sub-tasks" },
  { id: "Hygiene", title: "Triage and housekeeping" },
];

/**
 * TemplateGallery lists ready-made rules by theme. Picking one opens the rule dialog pre-filled;
 * templates already used (same rule name) and ones naming statuses the project lacks are marked.
 */
export function TemplateGallery({ onPick, existingNames, statusNames }: { onPick: (t: RuleTemplate) => void; existingNames: Set<string>; statusNames: string[] | null }) {
  const known = statusNames ? new Set(statusNames.map((s) => s.toLowerCase())) : null;
  return (
    <div className="flex flex-col gap-5">
      {GROUPS.map((g) => (
        <section key={g.id} aria-labelledby={`tpl-${g.id}`}>
          <h3 id={`tpl-${g.id}`} className="mb-2 text-[11px] font-semibold tracking-wide text-faint uppercase">
            {g.title}
          </h3>
          <ul className="grid gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
            {RULE_TEMPLATES.filter((t) => t.group === g.id).map((t) => {
              const used = existingNames.has(t.rule.name.toLowerCase());
              const missing = known ? namedStatuses(t.rule).filter((s) => !known.has(s.toLowerCase())) : [];
              const Icon = t.icon;
              return (
                <li key={t.id}>
                  <button
                    type="button"
                    onClick={() => onPick(t)}
                    aria-label={`Use template: ${t.title}`}
                    title={sentence(describeRule(t.rule))}
                    className={cn(
                      "flex h-full w-full flex-col gap-2 rounded-lg border bg-surface p-3.5 text-left transition-colors hover:border-accent hover:shadow-sm",
                      used ? "border-line" : "border-line-strong/60",
                    )}
                  >
                    <span className="flex items-start gap-2.5">
                      <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent">
                        <Icon className="size-4" />
                      </span>
                      <span className="text-sm leading-snug font-medium">{t.title}</span>
                    </span>
                    <span className="text-[13px] text-muted">{t.description}</span>
                    <span className="mt-auto flex flex-wrap gap-1.5 pt-1">
                      {used && (
                        <Badge tone="success">
                          <Check className="size-3" /> In use
                        </Badge>
                      )}
                      {missing.length > 0 && (
                        <span title="Pick another status when reviewing the rule">
                          <Badge tone="warning" className="normal-case">
                            <TriangleAlert className="size-3" /> No “{missing.join("”, “")}” status
                          </Badge>
                        </span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
}
