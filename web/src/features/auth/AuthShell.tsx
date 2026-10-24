import type { ReactNode } from "react";
import { FolderKanban } from "lucide-react";
import { ThemeMenu } from "@/layout/ThemeMenu";

/** AuthShell is the centred card of the signed-out pages (sign in, invitation, reset). */
export function AuthShell({ title, subtitle, children, below }: { title: ReactNode; subtitle?: ReactNode; children: ReactNode; below?: ReactNode }) {
  return (
    <div className="relative flex min-h-full items-center justify-center bg-canvas p-4">
      <div className="absolute top-3 right-3">
        <ThemeMenu />
      </div>
      <div className="w-full max-w-sm py-10">
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          <span className="flex size-11 items-center justify-center rounded-xl bg-accent text-accent-ink shadow-sm">
            <FolderKanban className="size-6" />
          </span>
          <div>
            <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
            {subtitle && <p className="mt-1 text-sm text-muted">{subtitle}</p>}
          </div>
        </div>
        <div className="rounded-lg border border-line bg-surface p-5 shadow-sm">{children}</div>
        {below && <div className="mt-4 text-center text-sm text-muted">{below}</div>}
      </div>
    </div>
  );
}
