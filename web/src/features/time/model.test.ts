import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { formatDuration } from "@/lib/duration";
import { autoRemaining, barSegments, cardTimeLabel, fromLocalInput, hours, isWeekend, periodRange, shiftPeriod, timeBar, timesheetCsv, toLocalInput } from "./model";

const H = 3600;
const fmt = (s: number) => formatDuration(s);

describe("timeBar", () => {
  it("is empty without estimates or work", () => {
    expect(timeBar(undefined).empty).toBe(true);
    expect(timeBar({ spentSeconds: 0 }).empty).toBe(true);
    expect(timeBar({ spentSeconds: 0, remainingSeconds: 0 }).empty).toBe(false);
  });
  it("splits the estimate into logged and remaining", () => {
    expect(timeBar({ originalSeconds: 8 * H, remainingSeconds: 6 * H, spentSeconds: 2 * H })).toEqual({ logged: 0.25, remaining: 0.75, estimateAt: undefined, over: 0, empty: false });
    // Less left than estimated: the bar still spans the estimate.
    expect(timeBar({ originalSeconds: 8 * H, remainingSeconds: 2 * H, spentSeconds: 2 * H })).toMatchObject({ logged: 0.25, remaining: 0.25, over: 0 });
  });
  it("marks the estimate when work runs over it", () => {
    expect(timeBar({ originalSeconds: 8 * H, remainingSeconds: 2 * H, spentSeconds: 8 * H })).toEqual({ logged: 0.8, remaining: 0.2, estimateAt: 0.8, over: 2 * H, empty: false });
    expect(timeBar({ originalSeconds: 4 * H, remainingSeconds: 0, spentSeconds: 8 * H })).toMatchObject({ logged: 1, estimateAt: 0.5, over: 4 * H });
  });
  it("works without an original estimate", () => {
    expect(timeBar({ remainingSeconds: 3 * H, spentSeconds: H })).toMatchObject({ logged: 0.25, remaining: 0.75, over: 0 });
  });
});

describe("barSegments", () => {
  it("draws logged then remaining on track", () => {
    expect(barSegments(timeBar({ originalSeconds: 8 * H, remainingSeconds: 6 * H, spentSeconds: 2 * H }))).toEqual([
      { from: 0, to: 0.25, kind: "logged" },
      { from: 0.25, to: 1, kind: "remaining" },
    ]);
  });
  it("marks logged work beyond the estimate", () => {
    expect(barSegments(timeBar({ originalSeconds: 4 * H, remainingSeconds: 0, spentSeconds: 8 * H }))).toEqual([
      { from: 0, to: 0.5, kind: "logged" },
      { from: 0.5, to: 1, kind: "logged-over" },
    ]);
  });
  it("marks remaining work that will overrun", () => {
    expect(barSegments(timeBar({ originalSeconds: 8 * H, remainingSeconds: 4 * H, spentSeconds: 6 * H }))).toEqual([
      { from: 0, to: 0.6, kind: "logged" },
      { from: 0.6, to: 0.8, kind: "remaining" },
      { from: 0.8, to: 1, kind: "remaining-over" },
    ]);
  });
});

describe("cardTimeLabel", () => {
  it("shows logged of estimated, else what is left or logged", () => {
    expect(cardTimeLabel({ originalSeconds: 8 * H, remainingSeconds: 5 * H, spentSeconds: 3 * H }, fmt)).toEqual({ text: "3h / 1d", title: "3h logged of 1d estimated, 5h remaining", over: false });
    expect(cardTimeLabel({ originalSeconds: 4 * H, remainingSeconds: 2 * H, spentSeconds: 3 * H }, fmt)?.over).toBe(true);
    expect(cardTimeLabel({ remainingSeconds: 5 * H, spentSeconds: 0 }, fmt)?.text).toBe("5h left");
    expect(cardTimeLabel({ spentSeconds: 90 * 60 }, fmt)?.text).toBe("1h 30m logged");
    expect(cardTimeLabel({ spentSeconds: 0 }, fmt)).toBeNull();
    expect(cardTimeLabel(undefined, fmt)).toBeNull();
  });
});

describe("helpers", () => {
  it("previews the automatic remaining estimate", () => {
    expect(autoRemaining(5 * H, 2 * H)).toBe(3 * H);
    expect(autoRemaining(H, 2 * H)).toBe(0);
    expect(autoRemaining(undefined, H)).toBeUndefined();
  });
  it("round-trips datetime-local values", () => {
    const d = new Date(2026, 9, 20, 9, 5);
    expect(toLocalInput(d)).toBe("2026-10-20T09:05");
    expect(fromLocalInput("2026-10-20T09:05")).toBe(d.toISOString());
    expect(fromLocalInput("nonsense")).toBeNull();
  });
  it("finds weeks and months", () => {
    expect(periodRange("week", "2026-10-21")).toEqual({ from: "2026-10-19", to: "2026-10-25" });
    expect(periodRange("week", "2026-10-25")).toEqual({ from: "2026-10-19", to: "2026-10-25" });
    expect(periodRange("month", "2026-02-10")).toEqual({ from: "2026-02-01", to: "2026-02-28" });
    expect(shiftPeriod("week", "2026-10-19", -1)).toEqual({ from: "2026-10-12", to: "2026-10-18" });
    expect(shiftPeriod("month", "2026-12-01", 1)).toEqual({ from: "2027-01-01", to: "2027-01-31" });
    expect(isWeekend("2026-10-24")).toBe(true);
    expect(isWeekend("2026-10-23")).toBe(false);
    expect(hours(0)).toBe("");
    expect(hours(5400)).toBe("1.5");
  });
});

describe("timesheetCsv", () => {
  it("writes a row per person and issue, totals, and defuses formulas", () => {
    const issue = (key: string, summary: string) => ({ id: key, key, summary, type: { id: "t", key: "task", name: "Task", icon: "", color: "", level: 0 }, status: { id: "s", name: "To Do", category: "todo" as const } });
    const ts: Schemas["Timesheet"] = {
      from: "2026-10-19",
      to: "2026-10-20",
      timeZone: "UTC",
      dates: ["2026-10-19", "2026-10-20"],
      dayTotals: [3 * H, 1800],
      total: 3 * H + 1800,
      people: [
        {
          user: { id: "u", displayName: "Ada, Countess", email: "ada@example.com", kind: "human" },
          days: [3 * H, 1800],
          total: 3 * H + 1800,
          issues: [
            { issue: issue("TS-1", '=HYPERLINK("x")'), days: [3 * H, 0], total: 3 * H },
            { issue: issue("TS-2", "Plain"), days: [0, 1800], total: 1800 },
          ],
        },
      ],
    };
    expect(timesheetCsv(ts).split("\r\n")).toEqual([
      "Person,Email,Issue,Summary,2026-10-19,2026-10-20,Total (hours)",
      '"Ada, Countess",ada@example.com,TS-1,"\'=HYPERLINK(""x"")",3,0,3',
      '"Ada, Countess",ada@example.com,TS-2,Plain,0,0.5,0.5',
      '"Ada, Countess",ada@example.com,,Total,3,0.5,3.5',
      "Everyone,,,Total,3,0.5,3.5",
      "",
    ]);
  });
});
