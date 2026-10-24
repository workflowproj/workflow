import { createContext, useContext, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowUpRight } from "lucide-react";
import { ErrorBox } from "@/components/ErrorBox";
import { Spinner } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { Gadget } from "./model";

/**
 * GadgetContext tells a gadget where it sits: the gadget itself, the edit-mode chrome to show
 * in its header (drag handle, menu) and how to open an issue in the page's drawer.
 */
export type GadgetCtx = { gadget: Gadget; handle?: ReactNode; controls?: ReactNode; openIssue: (key: string) => void };

export const GadgetContext = createContext<GadgetCtx | null>(null);

export function useGadget(): GadgetCtx {
  const ctx = useContext(GadgetContext);
  if (!ctx) throw new Error("useGadget outside GadgetContext");
  return ctx;
}

/**
 * GadgetFrame is a gadget's card: title (the custom one, else derived), a one-line scope
 * subtitle, a link out and the body. Bodies fade while new settings load, keeping the frame.
 */
export function GadgetFrame({ title, subtitle, link, dimmed, children }: { title: string; subtitle?: ReactNode; link?: ReactNode; dimmed?: boolean; children: ReactNode }) {
  const { gadget, handle, controls } = useGadget();
  const name = gadget.title?.trim() || title;
  return (
    <section aria-label={name} data-gadget-id={gadget.id} className="@container flex h-full min-w-0 flex-col rounded-lg border border-line bg-surface">
      <header className="flex min-h-14 items-start gap-1.5 border-b border-line py-2.5 pr-2 pl-4">
        {handle}
        <div className="min-w-0 flex-1 py-0.5">
          <h2 className="truncate text-sm leading-5 font-semibold" title={name}>
            {name}
          </h2>
          {subtitle && <p className="truncate text-xs leading-4 text-muted">{subtitle}</p>}
        </div>
        {controls ?? link}
      </header>
      <div className={cn("min-h-0 flex-1 p-4 transition-opacity", dimmed && "opacity-60")}>{children}</div>
    </section>
  );
}

const linkCls = "mt-0.5 inline-flex h-7 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted hover:bg-surface-2 hover:text-ink focus-visible:outline-2 focus-visible:outline-accent";

/** SearchLink opens the gadget's issues in the issue navigator. */
export function SearchLink({ jql }: { jql: string }) {
  return (
    <Link to="/issues" search={{ jql: jql || undefined }} className={linkCls} title="Open in search">
      <span className="hidden @xs:inline">Open in search</span>
      <ArrowUpRight aria-hidden className="size-3.5" />
      <span className="sr-only @xs:hidden">Open in search</span>
    </Link>
  );
}

/** ReportLink opens the project's reports page (sprint gadgets and velocity). */
export function ReportLink({ projectKey }: { projectKey: string }) {
  return (
    <Link to="/projects/$projectKey/reports" params={{ projectKey }} className={linkCls} title="Open reports">
      <span className="hidden @xs:inline">Reports</span>
      <ArrowUpRight aria-hidden className="size-3.5" />
      <span className="sr-only @xs:hidden">Open reports</span>
    </Link>
  );
}

/** GadgetMessage fills a gadget's body when there's nothing to chart (or nothing configured). */
export function GadgetMessage({ icon, title, body }: { icon?: ReactNode; title: string; body?: ReactNode }) {
  return (
    <div className="flex h-full min-h-40 flex-col items-center justify-center gap-1.5 px-4 py-6 text-center">
      {icon && <div className="mb-1 text-faint">{icon}</div>}
      <p className="text-sm font-medium">{title}</p>
      {body && <p className="max-w-xs text-[13px] text-muted">{body}</p>}
    </div>
  );
}

export function GadgetLoading({ label = "Loading" }: { label?: string }) {
  return (
    <div role="status" className="flex h-full min-h-40 items-center justify-center gap-2 text-sm text-muted">
      <Spinner />
      {label}…
    </div>
  );
}

/**
 * GadgetBody picks between a query's states: its error, a spinner on first load, the empty
 * message when there's nothing to show, else the content.
 */
export function GadgetBody<T>({
  query,
  empty,
  children,
}: {
  query: { data?: T; error: unknown; isLoading: boolean };
  empty?: (data: T) => ReactNode | false | null | undefined;
  children: (data: T) => ReactNode;
}) {
  if (query.error && !query.data) return <ErrorBox error={query.error} />;
  if (query.data === undefined) return <GadgetLoading />;
  const e = empty?.(query.data);
  return <>{e || children(query.data)}</>;
}
