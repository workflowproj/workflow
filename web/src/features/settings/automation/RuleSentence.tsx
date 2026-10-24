import { ArrowRight } from "lucide-react";
import { cn } from "@/lib/cn";
import type { RuleSentence } from "./describe";

/** RuleSentenceView renders "When … → action, action" with the actions as chips. */
export function RuleSentenceView({ sentence: s, className }: { sentence: RuleSentence; className?: string }) {
  return (
    <p className={cn("flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[13px] leading-relaxed", className)}>
      <span className="font-medium text-ink">{s.when}</span>
      {s.condition && (
        <>
          <span className="text-muted">if</span>
          <code className="rounded bg-surface-2 px-1 font-mono text-[12px]">{s.condition}</code>
        </>
      )}
      <ArrowRight className="size-3.5 shrink-0 text-faint" aria-label="then" />
      {s.actions.map((a, i) => (
        <span key={i} className="rounded bg-accent-soft px-1.5 py-0.5 text-accent">
          {a}
        </span>
      ))}
    </p>
  );
}
