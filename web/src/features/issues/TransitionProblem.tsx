import { CircleAlert, X } from "lucide-react";
import { ApiError, errorMessage } from "@/api/client";

/**
 * TransitionProblem explains why a transition failed: the headline (e.g. "PLAT-9 cannot move
 * to Done yet") and every validator or field message ("Finish every sub-task first.").
 */
export function TransitionProblem({ error, onDismiss }: { error: unknown; onDismiss?: () => void }) {
  if (!error) return null;
  const problem = error instanceof ApiError ? error.problem : undefined;
  const headline = problem?.detail || errorMessage(error);
  const items = (problem?.errors ?? []).map((e) => (e.field && e.field !== "transitionId" ? `${e.field}: ${e.message}` : e.message)).filter((m) => m && m !== headline);
  return (
    <div role="alert" className="flex gap-2.5 rounded-md border border-danger/30 bg-danger-soft px-3 py-2.5 text-sm text-danger">
      <CircleAlert className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{headline}</p>
        {items.length === 1 && <p className="mt-0.5 text-ink">{items[0]}</p>}
        {items.length > 1 && (
          <ul className="mt-1 list-disc pl-4 text-ink">
            {items.map((m, i) => (
              <li key={i}>{m}</li>
            ))}
          </ul>
        )}
      </div>
      {onDismiss && (
        <button type="button" aria-label="Dismiss" onClick={onDismiss} className="-mt-0.5 -mr-1 self-start rounded p-1 text-danger/70 hover:text-danger">
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}
