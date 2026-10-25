import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, ChevronDown } from "lucide-react";
import { ApiError } from "@/api/client";
import type { StatusCategory } from "@/api/types";
import { Popover, Spinner, useToast } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import { isConflict, transitionsQuery, useRunTransition, type AvailableTransition } from "./issue-api";
import { useIssue } from "./issue-context";
import { MenuKeys } from "./inline-edit";
import { TransitionScreenDialog } from "./TransitionScreenDialog";

const tone: Record<StatusCategory, string> = {
  todo: "bg-todo-soft text-todo",
  in_progress: "bg-progress-soft text-progress",
  done: "bg-done-soft text-done",
};

/**
 * StatusControl is the status button: it lists the transitions the workflow allows, runs those
 * without required screen fields straight away and opens the screen dialog for the rest.
 * `onProblem` receives validator failures so the view can explain them.
 */
export function StatusControl({ onProblem }: { onProblem: (err: unknown) => void }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const canTransition = issue.permissions.transition;
  const transitions = useQuery({ ...transitionsQuery(issue.key), enabled: canTransition });
  const run = useRunTransition(issue.key);
  const [screen, setScreen] = useState<AvailableTransition | null>(null);

  const choose = (t: AvailableTransition) => {
    onProblem(null);
    if (t.screen.some((f) => f.required)) {
      setScreen(t);
      return;
    }
    run.mutate(t, {
      onSuccess: (res) => toast.success(`${res.key} moved to ${res.status.name}`),
      onError: (err) => {
        if (err instanceof ApiError && (err.code === "screen_required" || err.code === "screen_invalid")) setScreen(t);
        else if (!isConflict(err)) onProblem(err);
        qc.invalidateQueries({ queryKey: transitionsQuery(issue.key).queryKey });
      },
    });
  };

  const label = (
    <>
      {run.isPending ? <Spinner className="size-3.5 text-current" /> : null}
      <span className="truncate">{issue.status.name}</span>
    </>
  );
  const base = "inline-flex h-8 max-w-56 items-center gap-1.5 rounded-md px-3 text-xs font-bold tracking-wide uppercase transition";

  if (!canTransition) {
    return <span className={cn(base, tone[issue.status.category], "cursor-default")}>{label}</span>;
  }
  return (
    <>
      <Popover
        trigger={({ toggle, open }) => (
          <button
            type="button"
            onClick={toggle}
            onPointerEnter={() => qc.prefetchQuery(transitionsQuery(issue.key))}
            aria-haspopup="menu"
            aria-expanded={open}
            aria-label={`Status: ${issue.status.name}. Change status`}
            className={cn(base, tone[issue.status.category], "hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent")}
            disabled={run.isPending}
          >
            {label}
            <ChevronDown className="size-3.5 shrink-0" strokeWidth={2.5} />
          </button>
        )}
        content={(close) => (
          <MenuKeys close={close} className="w-72">
            {transitions.isLoading ? (
              <div className="flex items-center gap-2 px-2.5 py-2 text-sm text-muted">
                <Spinner className="size-3.5" /> Loading transitions…
              </div>
            ) : transitions.data?.length ? (
              transitions.data.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  role="menuitem"
                  onClick={() => (close(), choose(t))}
                  className="flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2 focus:bg-surface-2 focus:outline-none"
                >
                  <span className="min-w-0 flex-1 truncate">{t.name}</span>
                  {t.screen.some((f) => f.required) && <span className="text-xs text-faint">…</span>}
                  <ArrowRight className="size-3 shrink-0 text-faint" />
                  <StatusBadge status={t.to} className="max-w-28" />
                </button>
              ))
            ) : (
              <p className="px-2.5 py-2 text-sm text-muted">No transitions are available from {issue.status.name}.</p>
            )}
          </MenuKeys>
        )}
      />
      <TransitionScreenDialog open={!!screen} transition={screen} issueKey={issue.key} onClose={() => setScreen(null)} />
    </>
  );
}
