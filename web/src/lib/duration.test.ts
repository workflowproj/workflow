import { describe, expect, it } from "vitest";
import { DEFAULT_UNITS, formatDuration, formatHours, parseDuration } from "./duration";

const secs = (text: string, units = DEFAULT_UNITS) => parseDuration(text, units).seconds;

describe("parseDuration", () => {
  it("reads the usual forms (the same cases as internal/timetrack)", () => {
    const cases: [string, number][] = [
      ["30m", 30 * 60],
      ["2h", 2 * 3600],
      ["1h 30m", 90 * 60],
      ["2h30m", 150 * 60],
      ["1.5h", 90 * 60],
      ["1,5h", 90 * 60],
      ["90", 90 * 60],
      ["0", 0],
      ["1d", 8 * 3600],
      ["1w", 40 * 3600],
      ["1w 2d 3h 30m", (40 + 16 + 3) * 3600 + 30 * 60],
      ["2 days", 16 * 3600],
      ["3 hours 15 minutes", 3 * 3600 + 15 * 60],
      ["  4H ", 4 * 3600],
      ["1h, 1h", 2 * 3600],
      ["30m 1h", 90 * 60],
      ["0.5d", 4 * 3600],
      ["1 hr 5 min", 65 * 60],
    ];
    for (const [text, want] of cases) expect(secs(text), text).toBe(want);
  });
  it("rejects anything else", () => {
    for (const bad of ["", "  ", "abc", "2x", "h", "1h junk", "-2h", "2mo", "1.2.3h"]) {
      expect(parseDuration(bad).error, bad).toBeTruthy();
    }
  });
  it("uses the configured working day and week", () => {
    expect(secs("1d", { hoursPerDay: 7.5, daysPerWeek: 4 })).toBe(27000);
    expect(secs("1w", { hoursPerDay: 7.5, daysPerWeek: 4 })).toBe(4 * 27000);
  });
});

describe("formatDuration", () => {
  it("writes weeks, days, hours and minutes", () => {
    const cases: [number, string][] = [
      [0, "0m"],
      [-60, "0m"],
      [29, "0m"],
      [30, "1m"],
      [90 * 60, "1h 30m"],
      [8 * 3600, "1d"],
      [9 * 3600, "1d 1h"],
      [40 * 3600, "1w"],
      [(40 + 16 + 3) * 3600 + 30 * 60, "1w 2d 3h 30m"],
      [40 * 3600 + 60, "1w 1m"],
    ];
    for (const [n, want] of cases) expect(formatDuration(n), String(n)).toBe(want);
    expect(formatDuration(undefined)).toBe("0m");
    expect(formatDuration(27000 + 3600, { hoursPerDay: 7.5, daysPerWeek: 5 })).toBe("1d 1h");
  });
  it("round-trips", () => {
    for (const s of ["1w 2d 3h 30m", "45m", "3d", "1w 4h", "2w 1d 7h 59m"]) expect(formatDuration(secs(s))).toBe(s);
  });
  it("formats hours for charts", () => {
    expect(formatHours(0)).toBe("0h");
    expect(formatHours(45000)).toBe("12.5h");
    expect(formatHours(3600 * 40)).toBe("40h");
  });
});
