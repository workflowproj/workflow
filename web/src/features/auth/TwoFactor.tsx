import { useEffect, useRef, useState, type ReactNode } from "react";
import { Download, ShieldCheck } from "lucide-react";
import type { Schemas } from "@/api/client";
import { Button, Checkbox, Field, Input } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, CopyButton } from "@/features/settings/common";

/** CodeInput takes a 6-digit authenticator code (or, with `recovery`, a recovery code). */
export function CodeInput({ id, value, onChange, recovery, autoFocus }: { id: string; value: string; onChange: (v: string) => void; recovery?: boolean; autoFocus?: boolean }) {
  return recovery ? (
    <Input id={id} value={value} onChange={(e) => onChange(e.target.value)} autoComplete="off" spellCheck={false} placeholder="xxxxx-xxxxx" autoFocus={autoFocus} className="font-mono tracking-wider" />
  ) : (
    <Input
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value.replace(/[^0-9]/g, "").slice(0, 6))}
      inputMode="numeric"
      autoComplete="one-time-code"
      placeholder="123456"
      autoFocus={autoFocus}
      className="font-mono text-base tracking-[0.3em]"
    />
  );
}

/**
 * useOnce runs fn once when the component mounts, even under StrictMode's double effects:
 * starting two-factor set-up twice would replace the secret the page shows.
 */
export function useOnce(fn: () => void) {
  const done = useRef(false);
  useEffect(() => {
    if (done.current) return;
    done.current = true;
    fn();
  }, [fn]);
}

/** groupKey shows a base32 secret in groups of four for typing into an app. */
function groupKey(s: string) {
  return s.replace(/(.{4})/g, "$1 ").trim();
}

/**
 * TotpSetupPanel shows the QR code and key of a new two-factor secret and confirms it with a
 * code from the app.
 */
export function TotpSetupPanel({
  setup,
  onConfirm,
  pending,
  error,
  submitLabel = "Turn on",
  footer,
}: {
  setup: Schemas["TotpSetup"];
  onConfirm: (code: string) => void;
  pending?: boolean;
  error?: unknown;
  submitLabel?: string;
  footer?: ReactNode;
}) {
  const [code, setCode] = useState("");
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        onConfirm(code);
      }}
    >
      <ol className="@container flex flex-col gap-4 text-sm">
        <li className="flex flex-col gap-3 @md:flex-row @md:items-start">
          {/* Natural size (whole pixels per module) keeps every module crisp; the white
              background stays in dark mode so cameras can read it. */}
          <img src={setup.qrCode} alt="QR code for your authenticator app" className="max-w-full shrink-0 self-center rounded-md border border-line bg-white p-1 @md:self-start" />
          <div className="flex min-w-0 flex-col gap-2">
            <p>
              <span className="font-medium">1. Scan the QR code</span> with an authenticator app such as 1Password, Google Authenticator, Microsoft Authenticator or Authy.
            </p>
            <p className="text-[13px] text-muted">Can't scan it? Enter this key in the app instead (time-based, 6 digits):</p>
            <div className="flex items-center gap-2 rounded-md border border-line bg-surface-2 py-1 pr-1 pl-2.5">
              <code data-testid="totp-secret" data-secret={setup.secret} className="min-w-0 flex-1 font-mono text-[13px] select-all">
                {groupKey(setup.secret)}
              </code>
              <CopyButton value={setup.secret} label="Copy key" variant="ghost" />
            </div>
          </div>
        </li>
        <li>
          <Field label="2. Enter the 6-digit code the app shows">{(id) => <CodeInput id={id} value={code} onChange={setCode} autoFocus />}</Field>
        </li>
      </ol>
      {error ? <ErrorBox error={error} /> : null}
      <div className="flex items-center justify-end gap-2">
        {footer}
        <Button type="submit" variant="primary" loading={pending} disabled={code.length !== 6} icon={<ShieldCheck className="size-4" />}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}

/** RecoveryCodesPanel shows recovery codes once, with copy and download, until acknowledged. */
export function RecoveryCodesPanel({ codes, onDone, doneLabel = "Done" }: { codes: string[]; onDone: () => void; doneLabel?: string }) {
  const [saved, setSaved] = useState(false);
  const text = `Workflow recovery codes (${window.location.host})\nEach code works once if you lose your authenticator.\n\n${codes.join("\n")}\n`;
  return (
    <div className="flex flex-col gap-4">
      <Callout tone="warning" title="Save your recovery codes">
        If you lose your phone, each code lets you sign in once. They will not be shown again; keep them in a password manager or somewhere safe.
      </Callout>
      <ul data-testid="recovery-codes" className="grid grid-cols-2 gap-x-6 gap-y-1.5 rounded-md border border-line bg-surface-2 px-4 py-3 font-mono text-[13px]">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        <CopyButton value={codes.join("\n")} label="Copy codes" />
        <Button
          size="sm"
          icon={<Download className="size-3.5" />}
          onClick={() => {
            const a = document.createElement("a");
            a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
            a.download = "workflow-recovery-codes.txt";
            a.click();
            URL.revokeObjectURL(a.href);
          }}
        >
          Download
        </Button>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-4">
        <Checkbox label="I have saved my recovery codes" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
        <Button variant="primary" disabled={!saved} onClick={onDone}>
          {doneLabel}
        </Button>
      </div>
    </div>
  );
}
