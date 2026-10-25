import { Badge, type Tone } from "@/components/ui";
import type { Schemas } from "@/api/client";

type Source = Schemas["UserSource"] | Schemas["GroupSource"];

const sources: Record<Source, { label: string; tone: Tone; hint: string }> = {
  google: { label: "Google", tone: "accent", hint: "Synced from Google Workspace" },
  scim: { label: "SCIM", tone: "accent", hint: "Provisioned by your identity provider over SCIM" },
  jit: { label: "JIT", tone: "neutral", hint: "Created on first sign-in" },
  admin: { label: "Admin", tone: "neutral", hint: "Created by an administrator" },
  dev: { label: "Dev", tone: "warning", hint: "Created by the development login" },
  system: { label: "System", tone: "neutral", hint: "Built-in account" },
  local: { label: "Local", tone: "neutral", hint: "Managed here" },
};

export function sourceLabel(s: Source) {
  return sources[s]?.label ?? s;
}

/** isDirectoryManaged: SCIM and Google directory sync own the profile; it is read-only here. */
export function isDirectoryManaged(s: Source) {
  return s === "scim" || s === "google";
}

export function SourceBadge({ source }: { source: Source }) {
  const s = sources[source] ?? { label: source, tone: "neutral" as Tone, hint: "" };
  return (
    <span title={s.hint}>
      <Badge tone={s.tone}>{s.label}</Badge>
    </span>
  );
}
