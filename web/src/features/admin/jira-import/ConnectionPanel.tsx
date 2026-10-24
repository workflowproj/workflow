import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { KeyRound, Trash2 } from "lucide-react";
import { Badge, Button, Card, Field, Input, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { When } from "@/features/settings/common";
import { useImportActions } from "./api";
import { busy, type JiraImport } from "./model";

/** ConnectionPanel is the Connect step of an existing import: the site, and its credentials. */
export function ConnectionPanel({ imp }: { imp: JiraImport }) {
  const actions = useImportActions(imp.id);
  const confirm = useConfirm();
  const toast = useToast();
  const navigate = useNavigate();
  const [email, setEmail] = useState(imp.email);
  const [token, setToken] = useState("");
  const [editing, setEditing] = useState(!imp.hasCredentials);
  const cloud = imp.flavor === "cloud";
  return (
    <div className="flex flex-col gap-4">
      <Card title="Jira site">
        <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-[10rem_1fr]">
          <dt className="text-muted">Site</dt>
          <dd className="font-mono text-[13px] break-all">{imp.baseUrl}</dd>
          <dt className="text-muted">Edition</dt>
          <dd>{cloud ? "Jira Cloud (REST v3)" : "Jira Data Center / Server (REST v2)"}</dd>
          <dt className="text-muted">Server</dt>
          <dd>{imp.serverTitle}</dd>
          {cloud && (
            <>
              <dt className="text-muted">Account</dt>
              <dd>{imp.email}</dd>
            </>
          )}
          <dt className="text-muted">Credentials</dt>
          <dd>{imp.hasCredentials ? <Badge tone="success">Stored, encrypted</Badge> : <Badge tone="warning">Deleted</Badge>}</dd>
          <dt className="text-muted">Connected</dt>
          <dd>
            <When iso={imp.createdAt} />
          </dd>
        </dl>
        <div className="flex flex-wrap gap-2 border-t border-line px-4 py-3">
          {!editing && (
            <Button icon={<KeyRound className="size-4" />} onClick={() => setEditing(true)} disabled={busy(imp)}>
              Replace token
            </Button>
          )}
          {imp.hasCredentials && (
            <Button
              variant="ghost"
              disabled={busy(imp)}
              loading={actions.deleteCredentials.isPending}
              onClick={async () => {
                if (await confirm({ title: "Delete the Jira credentials?", body: "The stored token is erased. Imported data and the mapping stay.", confirmLabel: "Delete credentials", danger: true }))
                  actions.deleteCredentials.mutate(undefined, { onSuccess: () => toast.success("Credentials deleted") });
              }}
            >
              Delete credentials
            </Button>
          )}
          <Button
            variant="ghost"
            className="ml-auto text-danger"
            icon={<Trash2 className="size-4" />}
            disabled={busy(imp)}
            loading={actions.remove.isPending}
            onClick={async () => {
              if (
                await confirm({
                  title: "Delete this import?",
                  body: "The import record, its mapping choices and credentials are deleted. Imported projects and issues stay, and importing the same site again still updates them instead of duplicating.",
                  confirmLabel: "Delete import",
                  danger: true,
                })
              )
                actions.remove.mutate(undefined, { onSuccess: () => navigate({ to: "/admin/import" }) });
            }}
          >
            Delete import
          </Button>
        </div>
      </Card>
      {editing && (
        <Card title={imp.hasCredentials ? "Replace the token" : "Enter a token to continue"}>
          <form
            className="flex flex-col gap-3 p-4"
            onSubmit={(e) => {
              e.preventDefault();
              actions.updateToken.mutate(
                { token: token.trim(), ...(cloud ? { email: email.trim() } : {}) },
                {
                  onSuccess: () => {
                    setToken("");
                    setEditing(false);
                    toast.success("Connection updated");
                  },
                },
              );
            }}
          >
            {cloud && <Field label="Account email">{(id) => <Input id={id} type="email" value={email} onChange={(e) => setEmail(e.target.value)} />}</Field>}
            <Field label={cloud ? "API token" : "Personal access token"} hint="Tested before it is stored.">
              {(id) => <Input id={id} type="password" autoComplete="new-password" value={token} onChange={(e) => setToken(e.target.value)} />}
            </Field>
            {actions.updateToken.error && <ErrorBox error={actions.updateToken.error} />}
            <div className="flex gap-2">
              <Button type="submit" variant="primary" disabled={!token.trim()} loading={actions.updateToken.isPending}>
                Test and save
              </Button>
              {imp.hasCredentials && (
                <Button variant="ghost" onClick={() => setEditing(false)}>
                  Cancel
                </Button>
              )}
            </div>
          </form>
        </Card>
      )}
    </div>
  );
}
