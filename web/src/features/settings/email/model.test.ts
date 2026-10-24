import { describe, expect, it } from "vitest";
import { applyPatch, formatHour, mergePatch, nextDigestLabel, preferencePatch, timeZoneOptions } from "./model";

describe("email notification settings", () => {
  it("formats digest hours as clock times", () => {
    expect(formatHour(0)).toBe("00:00");
    expect(formatHour(9)).toBe("09:00");
    expect(formatHour(23)).toBe("23:00");
  });

  it("lists UTC first and always includes the saved zone", () => {
    const zones = timeZoneOptions("Antarctica/Troll", () => ["Europe/London", "America/New_York"]);
    expect(zones[0]).toBe("UTC");
    expect(zones).toContain("Antarctica/Troll");
    expect(zones.slice(1)).toEqual(["America/New_York", "Antarctica/Troll", "Europe/London"]);
    expect(timeZoneOptions("UTC", () => ["UTC"])).toEqual(["UTC"]);
  });

  it("shows the next digest in the chosen zone", () => {
    const label = nextDigestLabel("2026-10-25T09:00:00Z", "Asia/Tokyo", "en-GB");
    expect(label).toContain("18:00");
    expect(label).toContain("25 Oct");
    expect(nextDigestLabel("2026-10-25T09:00:00Z", "UTC", "en-US")).toContain("09:00");
  });

  it("patches one kind at a time", () => {
    expect(preferencePatch("commented", "off")).toEqual({ preferences: [{ kind: "commented", delivery: "off" }] });
  });

  it("merges unsaved changes and shows them over the saved settings", () => {
    const unsaved = mergePatch(mergePatch(preferencePatch("commented", "off"), { paused: true }), mergePatch(preferencePatch("commented", "immediate"), preferencePatch("assigned", "digest")));
    expect(unsaved).toEqual({
      paused: true,
      preferences: [
        { kind: "commented", delivery: "immediate" },
        { kind: "assigned", delivery: "digest" },
      ],
    });
    const saved = {
      address: "a@example.com",
      digestHour: 9,
      timeZone: "UTC",
      paused: false,
      nextDigestAt: "2026-10-25T09:00:00Z",
      emailConfigured: true,
      preferences: [
        { kind: "assigned", label: "Assigned to me", description: "", delivery: "immediate" as const, defaultDelivery: "immediate" as const },
        { kind: "transitioned", label: "Status changes", description: "", delivery: "digest" as const, defaultDelivery: "digest" as const },
      ],
    };
    const shown = applyPatch(saved, unsaved);
    expect(shown.paused).toBe(true);
    expect(shown.preferences.map((p) => p.delivery)).toEqual(["digest", "digest"]);
    expect(applyPatch(saved, {})).toEqual(saved);
  });
});
