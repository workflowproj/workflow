import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { directorySchedule, directorySettings, directoryTitle, runStats } from "./directory";

const status = (s: Partial<Schemas["DirectorySyncStatus"]>): Schemas["DirectorySyncStatus"] => ({ configured: true, intervalMinutes: 15, runs: [], ...s });

describe("directory sync status", () => {
  it("names the configured directory", () => {
    expect(directoryTitle(status({ source: "keycloak" }))).toBe("Keycloak directory sync");
    expect(directoryTitle(status({ source: "google" }))).toBe("Google Workspace directory sync");
    expect(directoryTitle(status({ configured: false }))).toBe("Directory sync");
  });

  it("describes the schedule", () => {
    expect(directorySchedule(status({}))).toBe("Every 15 minutes");
    expect(directorySchedule(status({ intervalMinutes: 60 }))).toBe("Every hour");
    expect(directorySchedule(status({ intervalMinutes: 120 }))).toBe("Every 2 hours");
    expect(directorySchedule(status({ intervalMinutes: 0 }))).toBe("Manual runs only");
    expect(directorySchedule(status({ configured: false }))).toBe("Not configured");
  });

  it("lists Keycloak settings", () => {
    const rows = directorySettings(
      status({ source: "keycloak", realm: "acme", url: "https://sso.example.com", provider: "keycloak", group: "/workflow-users", syncGroups: true, groupPrefix: "wfm-" }),
    );
    expect(rows.map((r) => `${r.label}=${r.value}`)).toEqual([
      "Realm=acme",
      "Keycloak URL=https://sso.example.com",
      "Sign-in provider=keycloak",
      "Access group=/workflow-users",
      "Groups=Mirrored with their members",
      "Group name prefix=wfm-",
    ]);
    expect(directorySettings(status({ source: "keycloak", realm: "acme", syncGroups: false })).find((r) => r.label === "Access group")?.value).toBe("None: everyone in the realm");
    expect(directorySettings(status({ configured: false }))).toEqual([]);
  });

  it("lists Google settings", () => {
    const rows = directorySettings(status({ source: "google", group: "staff@example.com", orgUnit: "/Engineering", syncGroups: true }));
    expect(rows.map((r) => r.label)).toEqual(["Access group", "Org unit", "Groups"]);
  });

  it("orders and spells out run counters", () => {
    expect(runStats({ users: 5, membershipsAdded: 2, usersCreated: 3, custom_thing: 1 }).map((s) => `${s.label}: ${s.value}`)).toEqual([
      "users created: 3",
      "memberships added: 2",
      "users: 5",
      "custom thing: 1",
    ]);
  });
});
