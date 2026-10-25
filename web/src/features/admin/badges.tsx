import { Badge, type Tone } from "@/components/ui";
import type { Schemas } from "@/api/client";

type Source = Schemas["UserSource"] | Schemas["GroupSource"];

const sources: Record<Source, { label: string; tone: Tone; hint: string }> = {
  google: { label: "Google", tone: "accent", hint: "Synced from Google Workspace" },
  keycloak: { label: "Keycloak", tone: "accent", hint: "Synced from a Keycloak realm" },
  scim: { label: "SCIM", tone: "accent", hint: "Provisioned by your identity provider over SCIM" },
  oidc: { label: "Sign-in", tone: "accent", hint: "From the groups in people's sign-in tokens; updated at each sign-in" },
  jit: { label: "JIT", tone: "neutral", hint: "Created on first sign-in" },
  admin: { label: "Admin", tone: "neutral", hint: "Created by an administrator" },
  dev: { label: "Dev", tone: "warning", hint: "Created by the development login" },
  system: { label: "System", tone: "neutral", hint: "Built-in account" },
  local: { label: "Local", tone: "neutral", hint: "Managed here" },
};

export function sourceLabel(s: Source) {
  return sources[s]?.label ?? s;
}

/** isDirectoryManaged: SCIM and directory sync (Google, Keycloak) own the profile; it is read-only here. */
export function isDirectoryManaged(s: Source) {
  return s === "scim" || s === "google" || s === "keycloak";
}

export function SourceBadge({ source }: { source: Source }) {
  const s = sources[source] ?? { label: source, tone: "neutral" as Tone, hint: "" };
  return (
    <span title={s.hint}>
      <Badge tone={s.tone}>{s.label}</Badge>
    </span>
  );
}
