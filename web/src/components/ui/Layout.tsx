import type { ReactNode } from "react";
import { Link, type LinkProps } from "@tanstack/react-router";
import { cn } from "@/lib/cn";

export function PageHeader({ title, subtitle, actions, breadcrumbs }: { title: ReactNode; subtitle?: ReactNode; actions?: ReactNode; breadcrumbs?: ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        {breadcrumbs && <div className="mb-1 text-[13px] text-muted">{breadcrumbs}</div>}
        <h1 className="truncate text-xl font-semibold tracking-tight">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-muted">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Card({ children, className, title, actions }: { children: ReactNode; className?: string; title?: ReactNode; actions?: ReactNode }) {
  return (
    <section className={cn("rounded-lg border border-line bg-surface", className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
          <h2 className="text-sm font-semibold">{title}</h2>
          {actions}
        </header>
      )}
      {children}
    </section>
  );
}

export function EmptyState({ icon, title, body, action }: { icon?: ReactNode; title: string; body?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-6 py-12 text-center">
      {icon && <div className="mb-1 text-faint">{icon}</div>}
      <p className="font-medium">{title}</p>
      {body && <p className="max-w-md text-sm text-muted">{body}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/** TabLinks renders router links styled as tabs; the active one is underlined. */
export function TabLinks({ tabs }: { tabs: { label: ReactNode; to: LinkProps["to"]; params?: LinkProps["params"]; search?: LinkProps["search"]; exact?: boolean }[] }) {
  return (
    <nav className="flex gap-1 border-b border-line">
      {tabs.map((t, i) => (
        <Link
          key={i}
          to={t.to}
          params={t.params}
          search={t.search}
          activeOptions={{ exact: t.exact ?? false }}
          className="-mb-px border-b-2 border-transparent px-3 py-2 text-sm text-muted hover:text-ink"
          activeProps={{ className: "!border-accent !text-ink font-medium" }}
        >
          {t.label}
        </Link>
      ))}
    </nav>
  );
}

/** Table styles for plain HTML tables: <table className={table}> ... */
export const table = "w-full border-collapse text-sm [&_th]:border-b [&_th]:border-line [&_th]:px-3 [&_th]:py-2 [&_th]:text-left [&_th]:text-xs [&_th]:font-semibold [&_th]:text-muted [&_td]:border-b [&_td]:border-line [&_td]:px-3 [&_td]:py-2.5 [&_tr:last-child_td]:border-b-0";
