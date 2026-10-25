import { Bug, CircleHelp, FileText, KeyRound, Laptop, LifeBuoy, Repeat, ShieldCheck, Timer } from "lucide-react";
import { cn } from "@/lib/cn";
import type { Sla } from "./api";
import { slaStatus } from "./sla";

const icons: Record<string, typeof Bug> = {
  help: LifeBuoy,
  bug: Bug,
  laptop: Laptop,
  key: KeyRound,
  access: ShieldCheck,
  question: CircleHelp,
  change: Repeat,
  document: FileText,
};

export const REQUEST_TYPE_ICONS = Object.keys(icons);

/** RequestTypeIcon draws a request type's icon in a soft tile. */
export function RequestTypeIcon({ icon, size = "md", className }: { icon: string; size?: "sm" | "md" | "lg"; className?: string }) {
  const Icon = icons[icon] ?? LifeBuoy;
  const box = { sm: "size-6 rounded-md", md: "size-9 rounded-lg", lg: "size-11 rounded-lg" }[size];
  const glyph = { sm: "size-3.5", md: "size-5", lg: "size-6" }[size];
  return (
    <span aria-hidden className={cn("inline-flex shrink-0 items-center justify-center bg-accent-soft text-accent", box, className)}>
      <Icon className={glyph} />
    </span>
  );
}

const toneStyle = {
  success: "bg-success-soft text-success",
  warning: "bg-warning-soft text-warning",
  danger: "bg-danger-soft text-danger",
  neutral: "bg-surface-2 text-muted",
};

/** shortSlaName labels a clock in tight spaces (queue rows). */
export function shortSlaName(metric: Sla["metric"]) {
  return metric === "first_response" ? "Response" : "Resolution";
}

/** SlaBadge shows an SLA clock: time left, time over, paused, met or missed; `short` prefixes a compact name. */
export function SlaBadge({ sla, short, className }: { sla: Sla; short?: boolean; className?: string }) {
  const s = slaStatus(sla);
  return (
    <span title={s.detail} className={cn("inline-flex h-5 items-center gap-1 rounded px-1.5 text-xs font-semibold whitespace-nowrap tabular-nums", toneStyle[s.tone], className)}>
      <Timer aria-hidden className="size-3" />
      <span className="sr-only">{s.detail}</span>
      {short && (
        <span aria-hidden className="font-medium">
          {shortSlaName(sla.metric)}
        </span>
      )}
      <span aria-hidden>{s.label}</span>
    </span>
  );
}
