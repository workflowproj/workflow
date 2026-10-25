import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Upload, Wand2 } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Checkbox, Field, Input, Modal, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, CopyButton, CopyField, randomSecret } from "@/features/settings/common";
import { cn } from "@/lib/cn";

type Conn = Schemas["ScmConnection"];
type Provider = Schemas["ScmProvider"];

/**
 * ScmConnectionDialog creates a GitHub App or GitLab connection, or rotates the credentials of
 * an existing one (`connection`). The webhook secret is generated here so it can be copied into
 * the provider; the API never returns it.
 */
export function ScmConnectionDialog({ connection, onClose }: { connection?: Conn; onClose: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const editing = !!connection;
  const [provider, setProvider] = useState<Provider>(connection?.provider ?? "github");
  const [name, setName] = useState(connection?.name ?? "");
  const [appId, setAppId] = useState(connection?.appId ? String(connection.appId) : "");
  const [appSlug, setAppSlug] = useState(connection?.appSlug ?? "");
  const [privateKey, setPrivateKey] = useState("");
  const [token, setToken] = useState("");
  const [apiUrl, setApiUrl] = useState(connection?.apiUrl ?? "");
  const [secret, setSecret] = useState(editing ? "" : randomSecret());
  const [manageHooks, setManageHooks] = useState(connection?.manageHooks ?? true);
  const [writeBack, setWriteBack] = useState(connection?.writeBack ?? true);
  const [created, setCreated] = useState<Conn | null>(null);

  const save = useMutation({
    mutationFn: () => {
      if (connection) {
        const body: Schemas["ScmConnectionPatch"] = { name: name.trim(), manageHooks, writeBack };
        if (privateKey.trim()) body.privateKey = privateKey.trim();
        if (token.trim()) body.token = token.trim();
        if (secret.trim()) body.webhookSecret = secret.trim();
        return unwrap(api.PATCH("/admin/scm-connections/{connectionId}", { params: { path: { connectionId: connection.id } }, body }));
      }
      const body: Schemas["ScmConnectionInput"] =
        provider === "github"
          ? {
              provider,
              name: name.trim(),
              appId: Number(appId),
              appSlug: appSlug.trim() || undefined,
              privateKey: privateKey.trim(),
              webhookSecret: secret,
              writeBack,
              ...(apiUrl.trim() ? { apiUrl: apiUrl.trim() } : {}),
            }
          : { provider, name: name.trim(), token: token.trim(), webhookSecret: secret, manageHooks, writeBack, ...(apiUrl.trim() ? { apiUrl: apiUrl.trim() } : {}) };
      return unwrap(api.POST("/admin/scm-connections", { body }));
    },
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["scm-connections"] });
      if (connection) {
        toast.success(`Saved ${c.name}`);
        onClose();
      } else setCreated(c);
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};

  if (created) {
    return (
      <Modal
        open
        onClose={onClose}
        title={`${created.name} connected`}
        size="lg"
        footer={
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        }
      >
        <div className="flex flex-col gap-4 text-sm">
          <p className="text-muted">Repository discovery has started. Finish the setup in {created.provider === "github" ? "the GitHub App settings" : "GitLab"}:</p>
          <div>
            <p className="mb-1 text-[13px] font-medium">Webhook URL</p>
            <CopyField value={created.webhookUrl} />
          </div>
          {secret && (
            <div>
              <p className="mb-1 text-[13px] font-medium">Webhook secret</p>
              <CopyField value={secret} />
              <p className="mt-1 text-xs text-muted">Shown only now. Deliveries signed with another secret are rejected.</p>
            </div>
          )}
          {created.provider === "github" ? (
            <Callout title="GitHub App settings">
              Set the App's webhook URL and secret to the values above, subscribe to Push, Create, Delete, Pull request, Pull request review and Workflow run events, and install the App on the
              repositories to track. Write-back needs Pull requests: read & write.
            </Callout>
          ) : (
            <Callout title="GitLab">
              {created.manageHooks
                ? "Project hooks are created automatically on every repository the token can see."
                : "Add a group or project webhook with this URL and secret token, for Push, Merge request and Pipeline events."}
            </Callout>
          )}
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={editing ? `Edit ${connection.name}` : "Connect GitHub or GitLab"}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            type="submit"
            form="scm-form"
            loading={save.isPending}
            disabled={!name.trim() || (!editing && (provider === "github" ? !appId || !privateKey.trim() : !token.trim()))}
          >
            {editing ? "Save" : "Connect"}
          </Button>
        </>
      }
    >
      <form
        id="scm-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        {!editing && (
          <div role="radiogroup" aria-label="Provider" className="grid grid-cols-2 gap-2">
            {(
              [
                ["github", "GitHub App", "github.com or GitHub Enterprise Server"],
                ["gitlab", "GitLab", "gitlab.com or self-managed, with an access token"],
              ] as const
            ).map(([p, label, hint]) => (
              <button
                key={p}
                type="button"
                role="radio"
                aria-checked={provider === p}
                onClick={() => setProvider(p)}
                className={cn("rounded-md border p-3 text-left", provider === p ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
              >
                <span className="block text-sm font-medium">{label}</span>
                <span className="block text-xs text-muted">{hint}</span>
              </button>
            ))}
          </div>
        )}
        <Field label="Name" required error={fe.name}>
          {(id) => <Input id={id} autoFocus value={name} maxLength={100} onChange={(e) => setName(e.target.value)} placeholder={provider === "github" ? "Acme GitHub" : "GitLab"} />}
        </Field>
        {provider === "github" ? (
          <>
            {!editing && (
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="App ID" required error={fe.appId}>
                  {(id) => <Input id={id} inputMode="numeric" value={appId} onChange={(e) => setAppId(e.target.value.replace(/\D/g, ""))} placeholder="123456" />}
                </Field>
                <Field label="App slug" hint="From the App's public URL; used for install links.">
                  {(id) => <Input id={id} value={appSlug} onChange={(e) => setAppSlug(e.target.value)} placeholder="acme-workflow" />}
                </Field>
              </div>
            )}
            <Field
              label="Private key (PEM)"
              required={!editing}
              error={fe.privateKey}
              hint={editing ? "Leave empty to keep the current key." : "Generate one in the App settings and paste it or load the .pem file."}
            >
              {(id) => (
                <div className="flex flex-col gap-1.5">
                  <Textarea
                    id={id}
                    rows={5}
                    spellCheck={false}
                    value={privateKey}
                    onChange={(e) => setPrivateKey(e.target.value)}
                    className="font-mono text-[11px]"
                    placeholder={"-----BEGIN RSA PRIVATE KEY-----\n…\n-----END RSA PRIVATE KEY-----"}
                  />
                  <label className="inline-flex w-fit cursor-pointer items-center gap-1.5 text-[13px] text-accent hover:underline">
                    <Upload className="size-3.5" /> Load .pem file
                    <input
                      type="file"
                      accept=".pem,application/x-pem-file,text/plain"
                      className="sr-only"
                      onChange={async (e) => e.target.files?.[0] && setPrivateKey(await e.target.files[0].text())}
                    />
                  </label>
                </div>
              )}
            </Field>
            {!editing && (
              <Field label="API URL" hint="Leave empty for github.com; for GitHub Enterprise use https://HOST/api/v3." error={fe.apiUrl}>
                {(id) => <Input id={id} type="url" value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} placeholder="https://api.github.com" />}
              </Field>
            )}
          </>
        ) : (
          <>
            {!editing && (
              <Field label="GitLab URL" hint="Leave empty for gitlab.com." error={fe.apiUrl}>
                {(id) => <Input id={id} type="url" value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} placeholder="https://gitlab.example.com" />}
              </Field>
            )}
            <Field
              label="Access token"
              required={!editing}
              error={fe.token}
              hint={editing ? "Leave empty to keep the current token." : "A group or project access token with the api scope (Maintainer, to manage hooks)."}
            >
              {(id) => <Input id={id} type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} placeholder="glpat-…" />}
            </Field>
            <Checkbox label="Create and maintain project webhooks automatically" checked={manageHooks} onChange={(e) => setManageHooks(e.target.checked)} />
          </>
        )}
        <Field
          label="Webhook secret"
          hint={editing ? "Leave empty to keep the current secret; set a new one here and in the provider to rotate it." : "Enter the same value in the provider's webhook settings."}
        >
          {(id) => (
            <div className="flex gap-1.5">
              <Input id={id} value={secret} onChange={(e) => setSecret(e.target.value)} className="font-mono text-[12px]" autoComplete="off" />
              <Button icon={<Wand2 className="size-3.5" />} onClick={() => setSecret(randomSecret())}>
                Generate
              </Button>
              {secret && <CopyButton value={secret} size="md" />}
            </div>
          )}
        </Field>
        <Checkbox label="Write back: comment on pull requests with links to the issues they mention" checked={writeBack} onChange={(e) => setWriteBack(e.target.checked)} />
        {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}
