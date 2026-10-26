import { describe, expect, it } from "vitest";
import { addDays, daysBetween, formatRange, parseISODate, remainingLabel, sprintDatesProblem, toISODate } from "./dates";

describe("dates", () => {
  it("round-trips and adds days across months", () => {
    expect(toISODate(parseISODate("2026-10-23"))).toBe("2026-10-23");
    expect(addDays("2026-10-30", 3)).toBe("2026-11-02");
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
  });
  it("counts days", () => {
    expect(daysBetween("2026-10-19", "2026-11-02")).toBe(14);
    expect(daysBetween("2026-11-02", "2026-10-19")).toBe(-14);
  });
  it("labels the time left", () => {
    expect(remainingLabel("2026-11-02", "2026-10-23")).toEqual({ text: "10 days left", overdue: false });
    expect(remainingLabel("2026-10-24", "2026-10-23")?.text).toBe("1 day left");
    expect(remainingLabel("2026-10-23", "2026-10-23")?.text).toBe("Ends today");
    expect(remainingLabel("2026-10-21", "2026-10-23")).toEqual({ text: "2 days overdue", overdue: true });
    expect(remainingLabel(undefined)).toBeNull();
  });
  it("formats ranges", () => {
    const y = new Date().getFullYear();
    expect(formatRange(`${y}-10-19`, `${y}-11-02`)).toMatch(/19.*–.*2/);
    expect(formatRange()).toBe("");
  });
  it("says why sprint dates can't start a sprint", () => {
    expect(sprintDatesProblem("2026-10-19", "2026-11-02")).toBeUndefined();
    expect(sprintDatesProblem("", "2026-11-02")).toEqual({ field: "startDate", message: "Choose a start date." });
    expect(sprintDatesProblem("2026-10-19", "")).toEqual({ field: "endDate", message: "Choose an end date." });
    expect(sprintDatesProblem("2026-10-19", "2026-10-19")?.message).toBe("The end date must be after the start date.");
    expect(sprintDatesProblem("2026-10-19", "2026-10-12")?.field).toBe("endDate");
  });
});
