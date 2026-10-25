import { useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { CircleAlert, CircleCheck } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Spinner } from "@/components/ui";
import { useDebounced } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { targetOfPath, type WorkflowDoc } from "./model";
import type { PathError } from "./panel-bits";
import type { Selection } from "./useWorkflowEditor";

/** useWorkflowValidation validates the draft on the server as it changes (debounced). */
export function useWorkflowValidation(yaml: string, workflowId: string | undefined, enabled = true) {
  const debounced = useDebounced(yaml, 450);
  const q = useQuery({
    queryKey: ["workflow-validate", workflowId ?? "", debounced],
    queryFn: () => unwrap(api.POST("/workflows/validate", { body: { yaml: debounced, workflowId } })),
    enabled: enabled && debounced.trim() !== "",
    placeholderData: keepPreviousData,
    staleTime: 60_000,
    retry: false,
  });
  const pending = debounced !== yaml || q.isFetching;
  const errors: PathError[] = useMemo(() => q.data?.errors ?? [], [q.data]);
  return { result: q.data, errors, pending, requestError: q.error, upToDate: !pending && !!q.data && debounced === yaml };
}

/** errorTargets groups validation errors by the status / transition they concern. */
export function errorTargets(errors: PathError[]) {
  const statusErrors = new Map<number, string[]>();
  const transitionErrors = new Set<number>();
  for (const e of errors) {
    const t = targetOfPath(e.path);
    if (t.kind === "status") statusErrors.set(t.index, [...(statusErrors.get(t.index) ?? []), e.message]);
    if (t.kind === "transition") transitionErrors.add(t.index);
  }
  return { statusErrors, transitionErrors };
}

export function ValidationBadge({ pending, errorCount, parseError, onClick }: { pending: boolean; errorCount: number; parseError?: boolean; onClick?: () => void }) {
  const bad = parseError || errorCount > 0;
  return (
    <button
      type="button"
      onClick={onClick}
      aria-live="polite"
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs font-medium",
        pending && !parseError ? "border-line text-muted" : bad ? "border-danger/40 bg-danger-soft text-danger" : "border-success/40 bg-success-soft text-success",
      )}
    >
      {pending && !parseError ? <Spinner className="size-3" /> : bad ? <CircleAlert className="size-3.5" /> : <CircleCheck className="size-3.5" />}
      {parseError ? "YAML syntax error" : pending ? "Validating…" : bad ? `${errorCount} problem${errorCount === 1 ? "" : "s"}` : "Valid"}
    </button>
  );
}

/** ProblemsList shows validation errors; clicking one selects the element it concerns. */
export function ProblemsList({ errors, doc, onSelect, className }: { errors: PathError[]; doc: WorkflowDoc; onSelect: (s: Selection) => void; className?: string }) {
  if (errors.length === 0) return null;
  return (
    <div role="alert" className={cn("rounded-md border border-danger/30 bg-danger-soft/60", className)}>
      <p className="border-b border-danger/20 px-3 py-1.5 text-xs font-semibold text-danger">
        {errors.length} problem{errors.length === 1 ? "" : "s"} to fix before publishing
      </p>
      <ul className="max-h-40 overflow-y-auto py-1">
        {errors.map((e, i) => {
          const t = targetOfPath(e.path);
          const sel: Selection =
            t.kind === "status" && doc.statuses[t.index]
              ? { kind: "status", name: doc.statuses[t.index].name }
              : t.kind === "transition" && doc.transitions[t.index]
                ? { kind: "transition", index: t.index }
                : null;
          const what = t.kind === "status" ? `Status “${doc.statuses[t.index]?.name ?? t.index}”` : t.kind === "transition" ? `Transition “${doc.transitions[t.index]?.name ?? t.index}”` : null;
          return (
            <li key={i}>
              <button
                type="button"
                disabled={!sel}
                onClick={() => sel && onSelect(sel)}
                className="flex w-full flex-col items-start px-3 py-1 text-left text-[13px] hover:bg-danger-soft disabled:hover:bg-transparent"
              >
                <span className="text-ink">
                  {what && <span className="font-medium">{what}: </span>}
                  {e.message}
                </span>
                {e.path && <code className="font-mono text-[11px] text-muted">{e.path}</code>}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
