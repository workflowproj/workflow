import { describe, expect, it } from "vitest";
import { durationUnit, formatDuration, linear, niceTicks } from "./chart-utils";

describe("niceTicks", () => {
  it("rounds the top up to a clean step", () => {
    expect(niceTicks(26)).toEqual([0, 10, 20, 30]);
    expect(niceTicks(23, 5)).toEqual([0, 5, 10, 15, 20, 25]);
    expect(niceTicks(4)).toEqual([0, 1, 2, 3, 4]);
    expect(niceTicks(0.6)).toEqual([0, 0.2, 0.4, 0.6]);
  });
  it("handles empty data", () => {
    expect(niceTicks(0)).toEqual([0, 1]);
  });
});

describe("linear", () => {
  it("maps and inverts ranges", () => {
    const y = linear([0, 10], [100, 0]);
    expect(y(0)).toBe(100);
    expect(y(5)).toBe(50);
    expect(linear([3, 3], [0, 10])(3)).toBe(0);
  });
});

describe("formatDuration", () => {
  it("chooses units", () => {
    expect(durationUnit(30)).toBe("h");
    expect(durationUnit(100)).toBe("d");
    expect(formatDuration(0.5)).toBe("30m");
    expect(formatDuration(5.55)).toBe("5.6h");
    expect(formatDuration(30)).toBe("30h");
    expect(formatDuration(80)).toBe("3.3d");
    expect(formatDuration(12, "d")).toBe("0.5d");
    expect(formatDuration(0)).toBe("0h");
  });
});
