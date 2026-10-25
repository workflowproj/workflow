import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckCircle2, CircleAlert, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { errorMessage } from "@/api/client";

/** An optional button on a toast, e.g. "Open" after creating something. */
export type ToastAction = { label: string; onClick: () => void };
type Toast = { id: number; tone: "success" | "error" | "info"; text: string; action?: ToastAction };
type Ctx = { success: (t: string, action?: ToastAction) => void; error: (e: unknown) => void; info: (t: string, action?: ToastAction) => void };

const ToastContext = createContext<Ctx | null>(null);
let seq = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const push = useCallback((tone: Toast["tone"], text: string, action?: ToastAction) => {
    const id = ++seq;
    setToasts((t) => [...t, { id, tone, text, action }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), tone === "error" || action ? 7000 : 3500);
  }, []);
  const ctx: Ctx = {
    success: useCallback((t: string, a?: ToastAction) => push("success", t, a), [push]),
    info: useCallback((t: string, a?: ToastAction) => push("info", t, a), [push]),
    error: useCallback((e: unknown) => push("error", errorMessage(e)), [push]),
  };
  return (
    <ToastContext.Provider value={ctx}>
      {children}
      {createPortal(
        <div aria-live="polite" className="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-80 flex-col gap-2">
          {toasts.map((t) => (
            <div key={t.id} className={cn("pointer-events-auto flex items-start gap-2 rounded-md border bg-surface px-3 py-2.5 text-sm shadow-lg", t.tone === "error" ? "border-danger/40" : "border-line")}>
              {t.tone === "error" ? <CircleAlert className="mt-0.5 size-4 shrink-0 text-danger" /> : <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" />}
              <span className="flex-1">{t.text}</span>
              {t.action && (
                <button
                  onClick={() => {
                    t.action!.onClick();
                    setToasts((x) => x.filter((y) => y.id !== t.id));
                  }}
                  className="font-medium text-accent hover:underline"
                >
                  {t.action.label}
                </button>
              )}
              <button aria-label="Dismiss" onClick={() => setToasts((x) => x.filter((y) => y.id !== t.id))} className="text-faint hover:text-ink">
                <X className="size-3.5" />
              </button>
            </div>
          ))}
        </div>,
        document.body,
      )}
    </ToastContext.Provider>
  );
}

export function useToast(): Ctx {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast outside ToastProvider");
  return ctx;
}
