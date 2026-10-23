import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { CheckCircle2, CircleAlert, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { errorMessage } from "@/api/client";

type Toast = { id: number; tone: "success" | "error" | "info"; text: string };
type Ctx = { success: (t: string) => void; error: (e: unknown) => void; info: (t: string) => void };

const ToastContext = createContext<Ctx | null>(null);
let seq = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const push = useCallback((tone: Toast["tone"], text: string) => {
    const id = ++seq;
    setToasts((t) => [...t, { id, tone, text }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), tone === "error" ? 7000 : 3500);
  }, []);
  const ctx: Ctx = {
    success: useCallback((t: string) => push("success", t), [push]),
    info: useCallback((t: string) => push("info", t), [push]),
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
