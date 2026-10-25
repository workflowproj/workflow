import type { Schemas } from "@/api/client";

type Status = Schemas["DirectorySyncStatus"];

/** A row of the directory sync settings: label, value, and whether the value is code (an id, URL or path). */
export type DirectorySetting = { label: string; value: string; code?: boolean };

const names = { google: "Google Workspace", keycloak: "Keycloak" } as const;

export function directoryName(source?: Schemas["DirectorySyncRun"]["source"]) {
  return source ? names[source] : "Directory";
}

export function directoryTitle(status: Status) {
  return status.configured ? `${directoryName(status.source)} directory sync` : "Directory sync";
}

export function directorySchedule(status: Status) {
  if (!status.configured) return "Not configured";
  if (status.intervalMinutes <= 0) return "Manual runs only";
  if (status.intervalMinutes % 60 === 0) {
    const h = status.intervalMinutes / 60;
    return h === 1 ? "Every hour" : `Every ${h} hours`;
  }
  return status.intervalMinutes === 1 ? "Every minute" : `Every ${status.intervalMinutes} minutes`;
}

const statOrder = [
  "usersCreated",
  "usersUpdated",
  "usersReactivated",
  "usersDeactivated",
  "usersDeleted",
  "usersSkipped",
  "groupsCreated",
  "groupsUpdated",
  "groupsDeleted",
  "membershipsAdded",
  "membershipsRemoved",
  "users",
  "groups",
  "memberships",
];

const totals = new Set(["users", "groups", "memberships"]);

/** runStats orders a run's counters (changes first, then totals) and spells their names out: usersCreated → "users created". */
export function runStats(stats: Record<string, number>): { key: string; label: string; value: number; total: boolean }[] {
  const rank = (k: string) => (statOrder.includes(k) ? statOrder.indexOf(k) : statOrder.length);
  return Object.entries(stats)
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([key, value]) => ({
      key,
      value,
      total: totals.has(key),
      label: key
        .replace(/([a-z])([A-Z])/g, "$1 $2")
        .replace(/_/g, " ")
        .toLowerCase(),
    }));
}

/** directorySettings describes what the configured directory sync reads, for the settings list. */
export function directorySettings(status: Status): DirectorySetting[] {
  if (!status.configured) return [];
  const out: DirectorySetting[] = [];
  if (status.source === "keycloak") {
    out.push({ label: "Realm", value: status.realm ?? "", code: true });
    out.push({ label: "Keycloak URL", value: status.url ?? "", code: true });
    out.push({ label: "Sign-in provider", value: status.provider ?? "", code: true });
    out.push(status.group ? { label: "Access group", value: status.group, code: true } : { label: "Access group", value: "None: everyone in the realm" });
  } else {
    if (status.group) out.push({ label: "Access group", value: status.group, code: true });
    if (status.orgUnit) out.push({ label: "Org unit", value: status.orgUnit, code: true });
    if (!status.group && !status.orgUnit) out.push({ label: "Access group", value: "None: everyone in the domain" });
  }
  out.push({ label: "Groups", value: status.syncGroups ? "Mirrored with their members" : "Not mirrored" });
  if (status.syncGroups && status.groupPrefix) {
    out.push({ label: status.source === "keycloak" ? "Group name prefix" : "Group email prefix", value: status.groupPrefix, code: true });
  }
  return out;
}
