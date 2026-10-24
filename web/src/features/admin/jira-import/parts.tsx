import { Check, CircleAlert, Info, TriangleAlert } from "lucide-react";
import { cn } from "@/lib/cn";
import { reachable, sortProblems, STEPS, type JiraImport, type Problem, type StepKey } from "./model";

/** Stepper shows the wizard's steps; steps that can be opened are buttons. */
export function Stepper({ imp, step, onStep }: { imp: JiraImport; step: StepKey; onStep: (s: StepKey) => void }) {
  const at = STEPS.findIndex((s) => s.key === step);
  return (
    <nav aria-label="Import steps" className="overflow-x-auto">
      <ol className="flex flex-wrap items-center gap-1">
        {STEPS.map((s, i) => {
          const open = reachable(imp, s.key);
          const current = s.key === step;
          const done = i < at;
          return (
            <li key={s.key} className="flex items-center gap-1">
              {i > 0 && <span aria-hidden className={cn("h-px w-6 sm:w-10", done || current ? "bg-accent/60" : "bg-line-strong")} />}
              <button
                type="button"
                disabled={!open}
                aria-current={current ? "step" : undefined}
                onClick={() => onStep(s.key)}
                className={cn(
                  "flex items-center gap-2 rounded-full py-1 pr-3 pl-1 text-sm transition-colors disabled:cursor-not-allowed",
                  current ? "bg-accent-soft font-medium text-accent" : open ? "text-ink hover:bg-surface-2" : "text-faint",
                )}
              >
                <span
                  className={cn(
                    "flex size-6 items-center justify-center rounded-full text-xs font-semibold",
                    current ? "bg-accent text-accent-ink" : done ? "bg-success-soft text-success" : "bg-surface-2 text-muted",
                  )}
                >
                  {done ? <Check className="size-3.5" aria-hidden /> : i + 1}
                </span>
                {s.label}
              </button>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

const severity = {
  error: { icon: CircleAlert, cls: "text-danger", label: "Blocks the import" },
  warning: { icon: TriangleAlert, cls: "text-warning", label: "Warning" },
  info: { icon: Info, cls: "text-accent", label: "Note" },
} as const;

/** ProblemList lists problems by severity with their examples. */
export function ProblemList({ problems, empty = "No problems found." }: { problems: Problem[]; empty?: string }) {
  if (problems.length === 0) return <p className="px-4 py-3 text-sm text-muted">{empty}</p>;
  return (
    <ul className="divide-y divide-line">
      {sortProblems(problems).map((p, i) => {
        const s = severity[p.severity];
        return (
          <li key={i} className="flex gap-2.5 px-4 py-2.5 text-sm">
            <s.icon className={cn("mt-0.5 size-4 shrink-0", s.cls)} aria-label={s.label} />
            <div className="min-w-0 flex-1">
              <p>
                {p.project && <span className="mr-1.5 font-mono text-xs text-muted">{p.project}</span>}
                {p.message}
                {p.count > 1 && <span className="ml-1.5 text-xs text-muted">×{p.count}</span>}
              </p>
              {p.examples && p.examples.length > 0 && <p className="mt-0.5 truncate text-xs text-muted">e.g. {p.examples.join(" · ")}</p>}
            </div>
          </li>
        );
      })}
    </ul>
  );
}

/** Stat is a labelled number. */
export function Stat({ label, value, hint }: { label: string; value: string | number; hint?: string }) {
  return (
    <div className="min-w-0 rounded-md border border-line bg-surface px-3 py-2.5">
      <p className="truncate text-xs text-muted">{label}</p>
      <p className="mt-0.5 text-lg font-semibold tabular-nums">{value}</p>
      {hint && <p className="truncate text-[11px] text-faint">{hint}</p>}
    </div>
  );
}

/** ProgressBar is an accessible progress bar. */
export function ProgressBar({ value, label, tone = "accent" }: { value: number; label: string; tone?: "accent" | "success" | "danger" }) {
  return (
    <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value} className="h-1.5 w-full overflow-hidden rounded-full bg-surface-3">
      <div
        className={cn("h-full rounded-full transition-[width] duration-500", { accent: "bg-accent", success: "bg-success", danger: "bg-danger" }[tone])}
        style={{ width: `${value}%` }}
      />
    </div>
  );
}
