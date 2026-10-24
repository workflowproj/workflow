import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";
import { useEscapeLayer } from "@/lib/escape";

/**
 * Popover renders `content` below `trigger` and closes on outside click or Escape.
 * `trigger` receives the open state and a toggle function. With `portal`, the content is placed
 * on the page body (above the trigger when there is no room below), so containers that scroll
 * or clip, such as tables, cannot cut it off.
 */
export function Popover({
  trigger,
  content,
  align = "start",
  className,
  open: controlled,
  onOpenChange,
  portal,
}: {
  trigger: (props: { open: boolean; toggle: () => void }) => ReactNode;
  content: (close: () => void) => ReactNode;
  align?: "start" | "end";
  className?: string;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  portal?: boolean;
}) {
  const [inner, setInner] = useState(false);
  const open = controlled ?? inner;
  const set = (v: boolean) => (onOpenChange ? onOpenChange(v) : setInner(v));
  const ref = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left?: number; right?: number } | null>(null);
  useEscapeLayer(open, () => set(false));
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!ref.current?.contains(t) && !panel.current?.contains(t)) set(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  });
  useLayoutEffect(() => {
    if (!open || !portal) {
      setPos(null);
      return;
    }
    const place = () => {
      const r = ref.current?.getBoundingClientRect();
      if (!r) return;
      const h = panel.current?.offsetHeight ?? 0;
      const up = r.bottom + 4 + h > window.innerHeight && r.top - 4 - h >= 0;
      setPos({ top: up ? r.top - 4 - h : r.bottom + 4, ...(align === "end" ? { right: window.innerWidth - r.right } : { left: r.left }) });
    };
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, portal, align]);
  const box = "z-30 min-w-48 rounded-md border border-line bg-surface p-1 shadow-lg";
  return (
    <div ref={ref} className="relative inline-block">
      {trigger({ open, toggle: () => set(!open) })}
      {open &&
        (portal ? (
          createPortal(
            <div ref={panel} style={pos ?? { top: 0, left: 0 }} className={cn("fixed text-left", box, !pos && "invisible", className)}>
              {content(() => set(false))}
            </div>,
            document.body,
          )
        ) : (
          <div className={cn("absolute top-full mt-1", box, align === "end" ? "right-0" : "left-0", className)}>{content(() => set(false))}</div>
        ))}
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
