import { describe, expect, it } from "vitest";
import type { Sla } from "./api";
import { formatSeconds, slaStatus } from "./sla";

const sla = (over: Partial<Sla>): Sla => ({
  metric: "resolution",
  name: "Time to resolution",
  state: "running",
  targetSeconds: 4 * 3600,
  elapsedSeconds: 0,
  remainingSeconds: 4 * 3600,
  breached: false,
  ...over,
});

describe("formatSeconds", () => {
  it("writes compact durations", () => {
    expect(formatSeconds(0)).toBe("0m");
    expect(formatSeconds(59)).toBe("0m");
    expect(formatSeconds(45 * 60)).toBe("45m");
    expect(formatSeconds(3 * 3600 + 20 * 60)).toBe("3h 20m");
    expect(formatSeconds(2 * 3600)).toBe("2h");
    // Working time: no days.
    expect(formatSeconds(2 * 86400 + 4 * 3600 + 59)).toBe("52h");
    expect(formatSeconds(-90 * 60)).toBe("1h 30m");
  });
});

describe("slaStatus", () => {
  it("counts down a running clock and warns when it is at risk", () => {
    expect(slaStatus(sla({}))).toMatchObject({ tone: "success", label: "4h" });
    expect(slaStatus(sla({ remainingSeconds: 50 * 60 }))).toMatchObject({ tone: "warning", label: "50m" });
    // At risk is at most an hour, even for long goals.
    expect(slaStatus(sla({ targetSeconds: 40 * 3600, remainingSeconds: 2 * 3600 })).tone).toBe("success");
  });

  it("shows how far over a breached clock is", () => {
    const s = slaStatus(sla({ remainingSeconds: -2 * 3600, breached: true }));
    expect(s).toMatchObject({ tone: "danger", label: "-2h" });
    expect(s.detail).toContain("breached 2h ago");
  });

  it("marks paused clocks", () => {
    expect(slaStatus(sla({ state: "paused", remainingSeconds: 3000 }))).toMatchObject({ tone: "neutral", label: "50m (paused)" });
  });

  it("says whether a stopped clock met its goal", () => {
    expect(slaStatus(sla({ state: "stopped", elapsedSeconds: 3600 }))).toMatchObject({ tone: "success", label: "Met" });
    const missed = slaStatus(sla({ state: "stopped", elapsedSeconds: 5 * 3600, breached: true }));
    expect(missed).toMatchObject({ tone: "danger", label: "Missed" });
    expect(missed.detail).toContain("by 1h");
  });
});
