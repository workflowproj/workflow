import { useState } from "react";
import { Plug, Unplug } from "lucide-react";
import type { Schemas } from "@/api/client";
import { Badge, EmptyState, IconButton, table, useConfirm } from "@/components/ui";
import { UserAvatar } from "@/components/UserAvatar";
import { When } from "@/features/settings/common";
import { cn } from "@/lib/cn";

export type ConnectedApp = Schemas["ConnectedApp"];
export type OAuthClient = Schemas["OAuthClient"];
export type OAuthScope = Schemas["OAuthScope"];

/** canWrite reports whether a set of OAuth scopes lets an app change issues. */
export const canWrite = (scopes: OAuthScope[]) => scopes.includes("mcp:write");

/** AccessBadge names what a connected app may do. */
export function AccessBadge({ scopes }: { scopes: OAuthScope[] }) {
  return canWrite(scopes) ? <Badge tone="accent">Read &amp; write</Badge> : <Badge>Read only</Badge>;
}

/** AppIcon shows an app's logo (https only, loaded without a referrer) or its initial. */
export function AppIcon({ client, size = 32, className }: { client: Pick<OAuthClient, "name" | "logoUri">; size?: number; className?: string }) {
  const [broken, setBroken] = useState(false);
  const style = { width: size, height: size };
  if (client.logoUri && !broken) {
    return (
      <img
        src={client.logoUri}
        alt=""
        referrerPolicy="no-referrer"
        onError={() => setBroken(true)}
        style={style}
        className={cn("shrink-0 rounded-lg border border-line bg-surface object-contain p-0.5", className)}
      />
    );
  }
  return (
    <span
      aria-hidden
      style={{ ...style, fontSize: Math.round(size * 0.45) }}
      className={cn("inline-flex shrink-0 items-center justify-center rounded-lg bg-surface-3 font-semibold text-ink", className)}
    >
      {client.name.trim().charAt(0).toUpperCase() || "?"}
    </span>
  );
}

/** AppName shows an app with the host that identifies it. */
export function AppName({ client }: { client: OAuthClient }) {
  return (
    <span className="flex min-w-0 items-center gap-2.5">
      <AppIcon client={client} size={28} />
      <span className="min-w-0">
        <span className="block truncate font-medium">{client.name}</span>
        <span className="block truncate text-xs text-muted" title={client.kind === "metadata" ? `Identified by ${client.id}` : "Registered itself; returns to this host"}>
          {client.host || "—"}
        </span>
      </span>
    </span>
  );
}

/**
 * ConnectedAppsTable lists apps (AI assistants) people allowed to act for them over OAuth.
 * `showUser` adds the person's column for administrators.
 */
export function ConnectedAppsTable({
  apps,
  onRevoke,
  revoking,
  showUser,
}: {
  apps: ConnectedApp[];
  onRevoke: (app: ConnectedApp) => void;
  revoking?: string;
  showUser?: boolean;
}) {
  const confirm = useConfirm();
  if (apps.length === 0) {
    return (
      <EmptyState
        icon={<Plug className="size-7" />}
        title="No connected apps"
        body={
          showUser
            ? "Nobody has connected an AI assistant yet."
            : "When you add Workflow to Claude or another assistant by its MCP address and approve access in your browser, the app appears here."
        }
      />
    );
  }
  return (
    <div className="overflow-x-auto">
      <table className={table}>
        <thead>
          <tr>
            <th>App</th>
            {showUser && <th>Person</th>}
            <th>Access</th>
            <th>Connected</th>
            <th>Last used</th>
            <th className="w-12">
              <span className="sr-only">Disconnect</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {apps.map((a) => (
            <tr key={a.id}>
              <td className="max-w-72">
                <AppName client={a.client} />
              </td>
              {showUser && (
                <td>
                  {a.user && (
                    <span className="flex items-center gap-2">
                      <UserAvatar user={a.user} size={22} />
                      <span className="truncate">{a.user.displayName}</span>
                    </span>
                  )}
                </td>
              )}
              <td>
                <AccessBadge scopes={a.scopes} />
              </td>
              <td className="text-muted">
                <When iso={a.createdAt} />
              </td>
              <td className="text-muted">
                <When iso={a.lastUsedAt} empty="Never" />
              </td>
              <td className="text-right">
                <IconButton
                  label={`Disconnect ${a.client.name}`}
                  size="sm"
                  disabled={revoking === a.id}
                  onClick={async () => {
                    const who = showUser && a.user ? `${a.user.displayName}'s ` : "";
                    if (
                      await confirm({
                        title: `Disconnect ${who}${a.client.name}?`,
                        body: `${a.client.name} loses access immediately: its tokens stop working and it has to ask again before it can act in Workflow.`,
                        confirmLabel: "Disconnect",
                        danger: true,
                      })
                    )
                      onRevoke(a);
                  }}
                >
                  <Unplug className="size-4" />
                </IconButton>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
