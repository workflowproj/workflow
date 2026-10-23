import { Loader2 } from "lucide-react";
import { cn } from "@/lib/cn";

export function Spinner({ className }: { className?: string }) {
  return <Loader2 aria-hidden className={cn("size-4 animate-spin text-muted", className)} />;
}

export function PageSpinner({ label = "Loading" }: { label?: string }) {
  return (
    <div role="status" className="flex h-full min-h-40 w-full items-center justify-center gap-2 text-muted">
      <Spinner />
      <span className="text-sm">{label}…</span>
    </div>
  );
}
