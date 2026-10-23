import { useEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/cn";
import { useEscapeLayer } from "@/lib/escape";

/**
 * Popover renders `content` below `trigger` and closes on outside click or Escape.
 * `trigger` receives the open state and a toggle function.
 */
export function Popover({
  trigger,
  content,
  align = "start",
  className,
  open: controlled,
  onOpenChange,
}: {
  trigger: (props: { open: boolean; toggle: () => void }) => ReactNode;
  content: (close: () => void) => ReactNode;
  align?: "start" | "end";
  className?: string;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const [inner, setInner] = useState(false);
  const open = controlled ?? inner;
  const set = (v: boolean) => (onOpenChange ? onOpenChange(v) : setInner(v));
  const ref = useRef<HTMLDivElement>(null);
  useEscapeLayer(open, () => set(false));
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) set(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  });
  return (
    <div ref={ref} className="relative inline-block">
      {trigger({ open, toggle: () => set(!open) })}
      {open && (
        <div className={cn("absolute top-full z-30 mt-1 min-w-48 rounded-md border border-line bg-surface p-1 shadow-lg", align === "end" ? "right-0" : "left-0", className)}>
          {content(() => set(false))}
        </div>
      )}
    </div>
  );
}

export function MenuItem({ onClick, children, danger, disabled, icon }: { onClick: () => void; children: ReactNode; danger?: boolean; disabled?: boolean; icon?: ReactNode }) {
  return (
    <button
      type="button"
      role="menuitem"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm hover:bg-surface-2 disabled:opacity-40",
        danger ? "text-danger" : "text-ink",
      )}
    >
      {icon}
      {children}
    </button>
  );
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className="px-2.5 pt-2 pb-1 text-[11px] font-semibold tracking-wide text-faint uppercase">{children}</div>;
}
