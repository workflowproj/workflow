import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, RotateCw, Trash2 } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, EmptyState, Field, IconButton, Input, Modal, table, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { CopyField, SecretOnce, SettingsSection, When } from "@/features/settings/common";
import { cn } from "@/lib/cn";

type ScimStatus = Schemas["ScimStatus"];
type Created = Schemas["ScimConnectionCreated"];

/** ScimSection manages SCIM connections: one bearer token per identity provider. */
export function ScimSection({ scim }: { scim: ScimStatus }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [creating, setCreating] = useState(false);
  const [issued, setIssued] = useState<{ conn: Created; rotated: boolean } | null>(null);
  const rotate = useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/admin/scim-connections/{connectionId}/rotate", { params: { path: { connectionId: id } } })),
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["provisioning"] });
      setIssued({ conn: c, rotated: true });
    },
    onError: toast.error,
  });
  const revoke = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE("/admin/scim-connections/{connectionId}", { params: { path: { connectionId: id } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["provisioning"] });
      toast.success("SCIM connection revoked");
    },
    onError: toast.error,
  });

  return (
    <SettingsSection
      title="SCIM provisioning"
      description="Your identity provider (Okta, Microsoft Entra ID, …) creates, updates and deactivates people and groups automatically."
      actions={
        <Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => setCreating(true)}>
          New connection
        </Button>
      }
    >
      <div className="flex flex-col gap-3 p-4">
        <div>
          <p className="mb-1 text-[13px] font-medium">SCIM base URL</p>
          <CopyField value={scim.baseUrl} />
        </div>
        {scim.connections.length === 0 ? (
          <EmptyState title="No SCIM connections" body="Create a connection to get a bearer token for your identity provider." />
        ) : (
          <div className="overflow-x-auto rounded-md border border-line">
            <table className={table}>
              <thead>
                <tr>
                  <th>Connection</th>
                  <th>Provisioned</th>
                  <th>Created</th>
                  <th>Last used</th>
                  <th className="w-20">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {scim.connections.map((c) => (
                  <tr key={c.id}>
                    <td>
                      <span className="block font-medium">{c.name}</span>
                      <code className="font-mono text-xs text-muted">{c.prefix}…</code>
                    </td>
                    <td className="text-muted">
                      {c.userCount ?? 0} people · {c.groupCount ?? 0} groups
                    </td>
                    <td className="text-muted">
                      <When iso={c.createdAt} />
                    </td>
                    <td className="text-muted">
                      <When iso={c.lastUsedAt} empty="Never" />
                    </td>
                    <td className="text-right whitespace-nowrap">
                      <IconButton
                        label={`Rotate the token of ${c.name}`}
                        size="sm"
                        onClick={async () => {
                          if (await confirm({ title: `Rotate the token of ${c.name}?`, body: "The current token stops working immediately; paste the new one into your identity provider.", confirmLabel: "Rotate token" })) rotate.mutate(c.id);
                        }}
                      >
                        <RotateCw className="size-4" />
                      </IconButton>
                      <IconButton
                        label={`Revoke ${c.name}`}
                        size="sm"
                        onClick={async () => {
                          if (await confirm({ title: `Revoke ${c.name}?`, body: "Provisioning from this identity provider stops. People and groups it created stay, but are no longer updated.", confirmLabel: "Revoke", danger: true }))
                            revoke.mutate(c.id);
                        }}
                      >
                        <Trash2 className="size-4" />
                      </IconButton>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {creating && (
        <CreateScimDialog
          onClose={() => setCreating(false)}
          onCreated={(c) => {
            setCreating(false);
            setIssued({ conn: c, rotated: false });
          }}
        />
      )}
      {issued && <ScimTokenDialog conn={issued.conn} rotated={issued.rotated} onClose={() => setIssued(null)} />}
    </SettingsSection>
  );
}

function CreateScimDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (c: Created) => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/admin/scim-connections", { body: { name: name.trim() } })),
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["provisioning"] });
      onCreated(c);
    },
  });
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  return (
    <Modal
      open
      onClose={onClose}
      title="New SCIM connection"
      size="sm"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="scim-form" loading={create.isPending} disabled={!name.trim()}>
            Create
          </Button>
        </>
      }
    >
      <form
        id="scim-form"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
        className="flex flex-col gap-3"
      >
        <Field label="Name" error={fe.name} hint="Usually the identity provider, e.g. “Okta”.">
          {(id) => <Input id={id} autoFocus value={name} maxLength={100} onChange={(e) => setName(e.target.value)} placeholder="Okta" />}
        </Field>
        {create.error && Object.keys(fe).length === 0 && <ErrorBox error={create.error} />}
      </form>
    </Modal>
  );
}

const guides: Record<string, { title: string; steps: string[] }> = {
  okta: {
    title: "Okta",
    steps: [
      "Applications → your app → General → enable SCIM provisioning.",
      "Provisioning → Integration: SCIM connector base URL = the base URL above; unique identifier field = userName.",
      "Supported actions: Push New Users, Push Profile Updates, Push Groups. Authentication mode: HTTP Header → paste the token.",
      "Test Connector Configuration, then enable Create / Update / Deactivate Users under Provisioning → To App.",
    ],
  },
  entra: {
    title: "Microsoft Entra ID",
    steps: [
      "Enterprise applications → your app → Provisioning → Get started → Automatic.",
      "Tenant URL = the base URL above; Secret Token = the token.",
      "Test connection, save, and review the attribute mappings (userName ← userPrincipalName, emails[work] ← mail).",
      "Assign users and groups to the app, then set Provisioning Status to On.",
    ],
  },
  google: {
    title: "Google Workspace",
    steps: [
      "Google Workspace only offers SCIM for apps in its catalogue; for this platform use Google directory sync below instead.",
      "Set WFM_GOOGLE_DIRECTORY_ENABLED=true with a service account (WFM_GOOGLE_SERVICE_ACCOUNT_KEY) that has domain-wide delegation, and an admin to impersonate (WFM_GOOGLE_ADMIN_SUBJECT).",
      "Optionally limit the sync to a group (WFM_GOOGLE_SYNC_GROUP) or an org unit (WFM_GOOGLE_SYNC_ORG_UNIT).",
    ],
  },
};

function ScimTokenDialog({ conn, rotated, onClose }: { conn: Created; rotated: boolean; onClose: () => void }) {
  const [tab, setTab] = useState<keyof typeof guides>("okta");
  return (
    <Modal
      open
      onClose={onClose}
      title={rotated ? `New token for ${conn.name}` : `${conn.name} connection created`}
      size="lg"
      footer={
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <div>
          <p className="mb-1 text-[13px] font-medium">SCIM base URL</p>
          <CopyField value={conn.baseUrl} />
        </div>
        <SecretOnce label="Bearer token" value={conn.token} />
        <div>
          <p className="mb-2 text-[13px] font-medium">Set up your identity provider</p>
          <div role="tablist" aria-label="Identity provider" className="mb-2 flex gap-1 border-b border-line">
            {Object.entries(guides).map(([k, g]) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={tab === k}
                onClick={() => setTab(k)}
                className={cn("-mb-px border-b-2 px-3 py-1.5 text-[13px]", tab === k ? "border-accent font-medium text-ink" : "border-transparent text-muted hover:text-ink")}
              >
                {g.title}
              </button>
            ))}
          </div>
          <ol className="list-decimal space-y-1 pl-5 text-[13px] text-muted">
            {guides[tab].steps.map((s) => (
              <li key={s}>{s}</li>
            ))}
          </ol>
          <p className="mt-2 text-xs text-muted">People and groups provisioned over SCIM are read-only here; deactivating them in the identity provider deactivates them here.</p>
        </div>
      </div>
    </Modal>
  );
}
