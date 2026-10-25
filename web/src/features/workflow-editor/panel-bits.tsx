import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

export type PathError = { path?: string; message: string };

/** errorAt returns the message of the first error whose path starts with `prefix + suffix`. */
export function errorAt(errors: PathError[], prefix: string, suffix = ""): string | undefined {
  return errors.find((e) => e.path?.startsWith(prefix + suffix))?.message;
}

export function PanelSection({ title, hint, action, children, className }: { title: ReactNode; hint?: ReactNode; action?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn("border-b border-line px-4 py-3.5 last:border-b-0", className)}>
      <div className="mb-2 flex items-center justify-between gap-2">
        <h3 className="text-[11px] font-semibold tracking-wide text-faint uppercase">{title}</h3>
        {action}
      </div>
      {hint && <p className="-mt-1 mb-2 text-xs text-muted">{hint}</p>}
      {children}
    </section>
  );
}

export function FieldError({ message }: { message?: string }) {
  return message ? <p className="mt-1 text-xs text-danger">{message}</p> : null;
}

/** RowCard frames one item of a repeatable list (validator, screen field, post-function). */
export function RowCard({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("relative flex flex-col gap-2 rounded-md border border-line bg-surface-2/50 p-2.5", className)}>{children}</div>;
}
