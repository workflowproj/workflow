import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { cn } from "@/lib/cn";
import { useEscapeLayer } from "@/lib/escape";
import { IconButton } from "./Button";

/** Moves focus into the panel when it opens and back to the trigger when it closes. */
function useFocusReturn(open: boolean, panel: React.RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement as HTMLElement | null;
    const first = panel.current?.querySelector<HTMLElement>("[autofocus], input, textarea, select, button:not([data-close])");
    (first ?? panel.current)?.focus();
    return () => previous?.focus?.();
  }, [open, panel]);
}

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  size = "md",
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
}) {
  const panel = useRef<HTMLDivElement>(null);
  useEscapeLayer(open, onClose);
  useFocusReturn(open, panel);
  if (!open) return null;
  const width = { sm: "max-w-sm", md: "max-w-lg", lg: "max-w-2xl", xl: "max-w-4xl" }[size];
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 pt-[8vh]" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={panel} tabIndex={-1} role="dialog" aria-modal="true" aria-label={typeof title === "string" ? title : undefined} className={cn("w-full rounded-lg border border-line bg-surface shadow-xl outline-none", width)}>
        <div className="flex items-center justify-between gap-4 border-b border-line px-5 py-3.5">
          <h2 className="text-base font-semibold">{title}</h2>
          <IconButton label="Close" size="sm" onClick={onClose} data-close>
            <X className="size-4" />
          </IconButton>
        </div>
        <div className="px-5 py-4">{children}</div>
        {footer && <div className="flex justify-end gap-2 border-t border-line px-5 py-3">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

/** Drawer is a right-hand panel, e.g. for an issue opened from a board. */
export function Drawer({ open, onClose, children, width = "max-w-3xl", label }: { open: boolean; onClose: () => void; children: ReactNode; width?: string; label: string }) {
  const panel = useRef<HTMLDivElement>(null);
  useEscapeLayer(open, onClose);
  useFocusReturn(open, panel);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-40 flex justify-end bg-black/25" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={panel} tabIndex={-1} role="dialog" aria-modal="true" aria-label={label} className={cn("h-full w-full overflow-y-auto border-l border-line bg-surface shadow-2xl outline-none", width)}>
        {children}
      </div>
    </div>,
    document.body,
  );
}
