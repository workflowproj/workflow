import { useEffect, useState, type ReactNode } from "react";
import { Check, CircleAlert, Copy, Info, KeyRound, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui";
import { cn } from "@/lib/cn";
import { formatDateTime, relativeTime } from "@/lib/format";

/** copyText writes to the clipboard, falling back to a hidden textarea where the API is unavailable. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  }
}

export function CopyButton({
  value,
  label = "Copy",
  size = "sm",
  variant = "secondary",
  className,
}: {
  value: string;
  label?: string;
  size?: "sm" | "md";
  variant?: "secondary" | "ghost" | "subtle";
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <Button
      size={size}
      variant={variant}
      className={className}
      aria-label={`${label}${copied ? " (copied)" : ""}`}
      icon={copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      onClick={async () => setCopied(await copyText(value))}
    >
      {copied ? "Copied" : label}
    </Button>
  );
}

/** CopyField shows a read-only value (a URL, an id) with a copy button. */
export function CopyField({ value, label, mono = true }: { value: string; label?: string; mono?: boolean }) {
  return (
    <div className="flex min-w-0 items-center gap-2 rounded-md border border-line bg-surface-2 py-1 pr-1 pl-2.5">
      <code className={cn("min-w-0 flex-1 truncate text-[13px]", mono ? "font-mono" : "")} title={value}>
        {value}
      </code>
      <CopyButton value={value} label={label ?? "Copy"} variant="ghost" />
    </div>
  );
}

/** SecretOnce presents a secret that the API returns only once. */
export function SecretOnce({ label, value, children }: { label: string; value: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning-soft px-3 py-2 text-sm text-warning">
        <KeyRound className="mt-0.5 size-4 shrink-0" />
        <p>Copy the {label.toLowerCase()} now and store it somewhere safe; it will not be shown again.</p>
      </div>
      <p className="text-[13px] font-medium">{label}</p>
      <div className="flex items-center gap-2 rounded-md border border-line-strong bg-surface-2 p-1.5 pl-3">
        <code data-testid="secret-value" className="min-w-0 flex-1 font-mono text-[13px] break-all select-all">
          {value}
        </code>
        <CopyButton value={value} />
      </div>
      {children}
    </div>
  );
}

const calloutTones = {
  info: { cls: "border-accent/25 bg-accent-soft/60 text-ink", icon: Info, iconCls: "text-accent" },
  warning: { cls: "border-warning/40 bg-warning-soft text-ink", icon: TriangleAlert, iconCls: "text-warning" },
  danger: { cls: "border-danger/30 bg-danger-soft text-ink", icon: CircleAlert, iconCls: "text-danger" },
};

export function Callout({ tone = "info", title, children, className, icon }: { tone?: keyof typeof calloutTones; title?: ReactNode; children?: ReactNode; className?: string; icon?: ReactNode }) {
  const t = calloutTones[tone];
  const Icon = t.icon;
  return (
    <div className={cn("flex gap-2.5 rounded-md border px-3 py-2.5 text-sm", t.cls, className)}>
      {icon ?? <Icon className={cn("mt-0.5 size-4 shrink-0", t.iconCls)} />}
      <div className="min-w-0 flex-1 [&_code]:rounded [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:font-mono [&_code]:text-[12px]">
        {title && <p className="font-medium">{title}</p>}
        {children && <div className={cn("text-muted", title && "mt-0.5")}>{children}</div>}
      </div>
    </div>
  );
}

/** Switch is an accessible on/off toggle. */
export function Switch({ checked, onChange, label, disabled, hideLabel }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean; hideLabel?: boolean }) {
  return (
    <label className={cn("inline-flex items-center gap-2 text-sm", disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer")}>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={hideLabel ? label : undefined}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cn(
          "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
          checked ? "bg-accent" : "bg-surface-3",
        )}
      >
        <span className={cn("inline-block size-4 rounded-full bg-white shadow transition-transform", checked ? "translate-x-4.5" : "translate-x-0.5")} />
      </button>
      {!hideLabel && <span>{label}</span>}
    </label>
  );
}

export function CodeBlock({ children, className }: { children: ReactNode; className?: string }) {
  return <pre className={cn("overflow-x-auto rounded-md border border-line bg-surface-2 px-3 py-2.5 font-mono text-[12px] leading-relaxed", className)}>{children}</pre>;
}

/** When shows a relative time with the full date as a tooltip. */
export function When({ iso, empty = "—" }: { iso?: string | null; empty?: string }) {
  if (!iso) return <span className="text-faint">{empty}</span>;
  return (
    <time dateTime={iso} title={formatDateTime(iso)} className="whitespace-nowrap">
      {relativeTime(iso)}
    </time>
  );
}

export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

/** SettingsSection is a titled block of a settings page with an optional description. */
export function SettingsSection({ title, description, actions, children, className }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn("rounded-lg border border-line bg-surface", className)}>
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold">{title}</h2>
          {description && <p className="mt-0.5 text-[13px] text-muted">{description}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </header>
      {children}
    </section>
  );
}

/** randomSecret returns a URL-safe random string, e.g. for webhook secrets typed into a provider. */
export function randomSecret(bytes = 24): string {
  const a = new Uint8Array(bytes);
  crypto.getRandomValues(a);
  return btoa(String.fromCharCode(...a))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
