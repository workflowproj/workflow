import { describe, expect, it } from "vitest";
import { fromDay, toDay } from "./model";
import { addMonths, gridLines, header, timeRange } from "./scale";

describe("scale", () => {
  const today = toDay("2026-10-24");
  it("covers the data and room around today, in whole months", () => {
    const r = timeRange([toDay("2027-02-11"), toDay("2026-09-24")], today);
    expect(fromDay(r.start)).toBe("2026-08-01");
    expect(fromDay(r.end)).toBe("2027-03-31");
    const empty = timeRange([], today);
    expect(fromDay(empty.start)).toBe("2026-08-01");
    expect(fromDay(empty.end)).toBe("2027-02-28");
  });
  it("adds months across years", () => {
    expect(fromDay(addMonths(toDay("2026-12-15"), 1))).toBe("2027-01-01");
    expect(fromDay(addMonths(toDay("2026-01-31"), -1))).toBe("2025-12-01");
  });
  it("builds week headers with weekends", () => {
    const h = header({ start: toDay("2026-10-24"), end: toDay("2026-11-02") }, "weeks");
    expect(h.top.map((s) => [fromDay(s.start), fromDay(s.end)])).toEqual([
      ["2026-10-24", "2026-10-31"],
      ["2026-11-01", "2026-11-02"],
    ]);
    expect(h.bottom).toHaveLength(10);
    expect(h.bottom[0]).toMatchObject({ label: "24", weekend: true });
    expect(h.bottom[2]).toMatchObject({ label: "26", weekend: false, strong: true });
  });
  it("groups months into years and quarters", () => {
    const range = { start: toDay("2026-11-01"), end: toDay("2027-04-30") };
    expect(header(range, "months").top.map((s) => s.label)).toEqual(["2026", "2027"]);
    expect(header(range, "quarters").top.map((s) => s.label)).toEqual(["Q4 2026", "Q1 2027", "Q2 2027"]);
    expect(header(range, "quarters").bottom).toHaveLength(6);
  });
  it("draws lines on Mondays or month starts", () => {
    const weeks = gridLines({ start: toDay("2026-10-24"), end: toDay("2026-11-10") }, "weeks");
    expect(weeks.map((l) => fromDay(l.day))).toEqual(["2026-10-26", "2026-11-02", "2026-11-09"]);
    const q = gridLines({ start: toDay("2026-11-01"), end: toDay("2027-02-28") }, "quarters");
    expect(q.map((l) => [fromDay(l.day), l.strong])).toEqual([
      ["2026-12-01", false],
      ["2027-01-01", true],
      ["2027-02-01", false],
    ]);
  });
});
