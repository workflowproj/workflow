import { useState } from "react";
import { KeyRound, Trash2 } from "lucide-react";
import { ApiError, type Schemas } from "@/api/client";
import { Badge, Button, Checkbox, EmptyState, Field, IconButton, Input, Modal, Select, table, useConfirm } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { formatDate } from "@/lib/format";
import { SecretOnce, When } from "../common";

export type ApiToken = Schemas["ApiToken"];
export type ApiTokenCreate = Schemas["ApiTokenCreate"];
export type ApiTokenCreated = Schemas["ApiTokenCreated"];
type Scope = ApiTokenCreate["scopes"][number];

const SCOPES: { id: Scope; label: string; hint: string }[] = [
  { id: "read", label: "Read", hint: "GET requests only: search, read issues and configuration." },
  { id: "write", label: "Write", hint: "Create and change issues, boards, workflows (wfm workflow apply)." },
  { id: "admin", label: "Admin", hint: "Organisation administration (only while you are an admin)." },
];
const EXPIRY = [7, 30, 90, 180, 366];

export function TokenTable({ tokens, onRevoke, revoking }: { tokens: ApiToken[]; onRevoke: (t: ApiToken) => void; revoking?: string }) {
  const confirm = useConfirm();
  if (tokens.length === 0) return <EmptyState icon={<KeyRound className="size-7" />} title="No tokens" body="Tokens let scripts, CI and the wfm CLI call the API." />;
  return (
    <div className="overflow-x-auto">
      <table className={table}>
        <thead>
          <tr>
            <th>Name</th>
            <th>Scopes</th>
            <th>Created</th>
            <th>Last used</th>
            <th>Expires</th>
            <th className="w-12">
              <span className="sr-only">Revoke</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {tokens.map((t) => {
            const expired = new Date(t.expiresAt).getTime() < Date.now();
            return (
              <tr key={t.id}>
                <td>
                  <span className="block font-medium">{t.name}</span>
                  <code className="font-mono text-xs text-muted">{t.prefix}…</code>
                </td>
                <td>
                  <span className="flex gap-1">
                    {t.scopes.map((s) => (
                      <Badge key={s} tone={s === "admin" ? "warning" : s === "write" ? "accent" : "neutral"}>
                        {s}
                      </Badge>
                    ))}
                  </span>
                </td>
                <td className="text-muted">
                  <When iso={t.createdAt} />
                </td>
                <td className="text-muted">
                  <When iso={t.lastUsedAt} empty="Never" />
                </td>
                <td>{expired ? <Badge tone="danger">Expired</Badge> : <span className="text-muted">{formatDate(t.expiresAt)}</span>}</td>
                <td className="text-right">
                  <IconButton
                    label={`Revoke ${t.name}`}
                    size="sm"
                    disabled={revoking === t.id}
                    onClick={async () => {
                      if (await confirm({ title: `Revoke “${t.name}”?`, body: "Anything using this token stops working immediately.", confirmLabel: "Revoke token", danger: true })) onRevoke(t);
                    }}
                  >
                    <Trash2 className="size-4" />
                  </IconButton>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/**
 * CreateTokenDialog asks for a name, scopes and expiry, then shows the new token once.
 * `create` performs the request (personal or service-account endpoint).
 */
export function CreateTokenDialog({
  open,
  onClose,
  create,
  allowAdmin,
  title = "New token",
  after,
}: {
  open: boolean;
  onClose: () => void;
  create: (body: ApiTokenCreate) => Promise<ApiTokenCreated>;
  allowAdmin: boolean;
  title?: string;
  /** Extra guidance shown with the new token (e.g. CLI usage). */
  after?: (token: string) => React.ReactNode;
}) {
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<Scope[]>(["read", "write"]);
  const [days, setDays] = useState(90);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [created, setCreated] = useState<ApiTokenCreated | null>(null);
  const fe = error instanceof ApiError ? error.fieldErrors() : {};
  const close = () => {
    setName("");
    setCreated(null);
    setError(null);
    onClose();
  };
  const submit = async () => {
    setPending(true);
    setError(null);
    try {
      setCreated(await create({ name: name.trim(), scopes, expiresInDays: days }));
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={close}
      title={created ? `Token “${created.name}” created` : title}
      size="lg"
      footer={
        created ? (
          <Button variant="primary" onClick={close}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" type="submit" form="token-form" loading={pending} disabled={!name.trim() || scopes.length === 0}>
              Create token
            </Button>
          </>
        )
      }
    >
      {created ? (
        <div className="flex flex-col gap-4">
          <SecretOnce label="Token" value={created.token} />
          <p className="text-[13px] text-muted">
            Send it as <code className="rounded bg-surface-2 px-1 font-mono text-[12px] text-ink">Authorization: Bearer {created.prefix}…</code>. It expires on {formatDate(created.expiresAt)}.
          </p>
          {after?.(created.token)}
        </div>
      ) : (
        <form
          id="token-form"
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Field label="Name" required error={fe.name} hint="What uses it, e.g. “GitHub Actions — workflows repo”.">
            {(id) => <Input id={id} autoFocus value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Scopes</legend>
            <div className="flex flex-col gap-2">
              {SCOPES.filter((s) => s.id !== "admin" || allowAdmin).map((s) => (
                <Checkbox
                  key={s.id}
                  checked={scopes.includes(s.id)}
                  onChange={(e) => setScopes(e.target.checked ? [...scopes, s.id] : scopes.filter((x) => x !== s.id))}
                  label={
                    <span>
                      <span className="font-medium">{s.label}</span> <span className="text-muted">— {s.hint}</span>
                    </span>
                  }
                />
              ))}
            </div>
            {fe.scopes && <p className="mt-1 text-xs text-danger">{fe.scopes}</p>}
          </fieldset>
          <Field label="Expires after" error={fe.expiresInDays}>
            {(id) => (
              <Select id={id} value={days} onChange={(e) => setDays(Number(e.target.value))} className="max-w-48">
                {EXPIRY.map((d) => (
                  <option key={d} value={d}>
                    {d === 366 ? "1 year" : `${d} days`}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          {error != null && Object.keys(fe).length === 0 && <ErrorBox error={error} />}
        </form>
      )}
    </Modal>
  );
}
