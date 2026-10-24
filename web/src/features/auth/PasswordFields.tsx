import { forwardRef, useState, type InputHTMLAttributes } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Input } from "@/components/ui";
import { useDebounced } from "@/features/settings/common";
import { cn } from "@/lib/cn";

/** PasswordInput is a password box with a show/hide toggle. */
export const PasswordInput = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean }>(function PasswordInput({ className, ...rest }, ref) {
  const [shown, setShown] = useState(false);
  return (
    <div className="relative">
      <Input ref={ref} type={shown ? "text" : "password"} spellCheck={false} autoCapitalize="off" className={cn("pr-9", className)} {...rest} />
      <button
        type="button"
        onClick={() => setShown(!shown)}
        aria-label={shown ? "Hide password" : "Show password"}
        aria-pressed={shown}
        className="absolute top-1/2 right-1 flex size-7 -translate-y-1/2 items-center justify-center rounded text-faint hover:bg-surface-2 hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
      >
        {shown ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  );
});

export const MIN_PASSWORD = 12;

const meter = [
  { label: "Not allowed", bar: "bg-danger", text: "text-danger" },
  { label: "Weak", bar: "bg-danger", text: "text-danger" },
  { label: "Fair", bar: "bg-warning", text: "text-warning" },
  { label: "Good", bar: "bg-success", text: "text-success" },
  { label: "Strong", bar: "bg-success", text: "text-success" },
];

/**
 * NewPasswordField is a password box with live strength feedback: the server rates the password
 * against the same policy it enforces (length, very common passwords, your name or email).
 */
export function NewPasswordField({
  id,
  value,
  onChange,
  email,
  name,
  error,
  autoFocus,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  email?: string;
  name?: string;
  error?: string;
  autoFocus?: boolean;
}) {
  const debounced = useDebounced(value, 250);
  const check = useQuery({
    queryKey: ["password-check", debounced, email ?? "", name ?? ""],
    queryFn: () => unwrap(api.POST("/auth/local/password-check", { body: { password: debounced, email, displayName: name } })),
    enabled: debounced.length > 0,
    staleTime: Infinity,
    retry: false,
    placeholderData: keepPreviousData,
  });
  const result = value.length > 0 && debounced.length > 0 ? check.data : undefined;
  const score = result?.score ?? 0;
  const m = meter[score];
  const short = value.length > 0 && value.length < MIN_PASSWORD;
  const problems = short ? [`Use at least ${MIN_PASSWORD} characters (${value.length} so far).`] : (result?.problems ?? []);
  return (
    <div className="flex flex-col gap-1.5">
      <PasswordInput
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="new-password"
        autoFocus={autoFocus}
        maxLength={128}
        invalid={!!error}
        aria-describedby={`${id}-strength`}
      />
      <div id={`${id}-strength`} aria-live="polite" className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          <div className="grid flex-1 grid-cols-4 gap-1" aria-hidden>
            {[1, 2, 3, 4].map((i) => (
              <span key={i} className={cn("h-1 rounded-full", value && !short && result && score >= i ? m.bar : "bg-surface-3")} />
            ))}
          </div>
          <span className={cn("w-20 text-right text-xs font-medium", value && result && !short ? m.text : "text-faint")}>
            {!value ? "" : short ? "Too short" : result ? `${m.label}` : "Checking…"}
          </span>
        </div>
        {error ? (
          <p className="text-xs text-danger">{error}</p>
        ) : problems.length > 0 ? (
          <ul className="flex flex-col gap-0.5 text-xs text-danger">
            {problems.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        ) : (
          <p className="text-xs text-muted">At least {MIN_PASSWORD} characters. A few unrelated words make a strong, memorable password.</p>
        )}
      </div>
    </div>
  );
}
