import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { IssueDetail } from "@/api/types";
import { cn } from "@/lib/cn";
import { issueQuery, useUpdateIssue, type IssuePatch } from "./issue-api";

type Ctx = {
  issue: IssueDetail;
  variant: "page" | "drawer";
  /** PATCH the issue; resolves to undefined when it failed (errors are already reported). */
  update: (patch: IssuePatch, optimistic?: Partial<IssueDetail>) => Promise<IssueDetail | undefined>;
  /** Show another issue: in place for a drawer, otherwise its page. */
  open: (key: string) => void;
  onClose?: () => void;
  /** Lets the toolbar ask sections to start an action (e.g. focus "add child"). */
  request: (action: IssueAction) => void;
  action: { type: IssueAction; seq: number } | null;
};

export type IssueAction = "add-child" | "add-link" | "attach";

const IssueContext = createContext<Ctx | null>(null);

export function IssueProvider({
  issue,
  variant,
  onNavigate,
  onClose,
  children,
}: {
  issue: IssueDetail;
  variant: "page" | "drawer";
  onNavigate?: (key: string) => void;
  onClose?: () => void;
  children: ReactNode;
}) {
  const [action, setAction] = useState<Ctx["action"]>(null);
  const request = useCallback((type: IssueAction) => setAction((a) => ({ type, seq: (a?.seq ?? 0) + 1 })), []);
  const mutation = useUpdateIssue(issue.key);
  const navigate = useNavigate();
  const { mutateAsync } = mutation;
  const update = useCallback((patch: IssuePatch, optimistic?: Partial<IssueDetail>) => mutateAsync({ patch, optimistic }).catch(() => undefined), [mutateAsync]);
  const open = useCallback((key: string) => (onNavigate ? onNavigate(key) : navigate({ to: "/browse/$issueKey", params: { issueKey: key } })), [onNavigate, navigate]);
  const value = useMemo(() => ({ issue, variant, update, open, onClose, request, action }), [issue, variant, update, open, onClose, request, action]);
  return <IssueContext.Provider value={value}>{children}</IssueContext.Provider>;
}

export function useIssue() {
  const c = useContext(IssueContext);
  if (!c) throw new Error("useIssue outside IssueProvider");
  return c;
}

/** useIssueAction runs `run` whenever the toolbar requests `type`. */
export function useIssueAction(type: IssueAction, run: () => void) {
  const { action } = useIssue();
  const seen = useRef(action?.seq ?? 0);
  const latest = useRef(run);
  latest.current = run;
  useEffect(() => {
    if (action && action.seq !== seen.current) {
      seen.current = action.seq;
      if (action.type === type) latest.current();
    }
  }, [action, type]);
}

/**
 * IssueKeyLink links to an issue page; inside a drawer a plain click switches the drawer instead
 * (modifier clicks still open the page in a new tab).
 */
export function IssueKeyLink({ issueKey, className, children }: { issueKey: string; className?: string; children?: ReactNode }) {
  const { open, variant } = useIssue();
  const qc = useQueryClient();
  return (
    <Link
      to="/browse/$issueKey"
      params={{ issueKey }}
      className={cn("hover:underline", className)}
      onMouseEnter={() => qc.prefetchQuery({ ...issueQuery(issueKey), staleTime: 10_000 })}
      onClick={(e) => {
        if (variant !== "drawer" || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        open(issueKey);
      }}
    >
      {children ?? issueKey}
    </Link>
  );
}

/** Section is a titled block in the issue body. */
export function Section({ title, count, actions, children, id }: { title: ReactNode; count?: number; actions?: ReactNode; children: ReactNode; id?: string }) {
  return (
    <section id={id} className="min-w-0">
      <header className="mb-2 flex min-h-7 items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          {title}
          {count !== undefined && count > 0 && <span className="rounded-full bg-surface-2 px-1.5 text-[11px] font-semibold text-muted">{count}</span>}
        </h2>
        {actions && <div className="flex items-center gap-1">{actions}</div>}
      </header>
      {children}
    </section>
  );
}
