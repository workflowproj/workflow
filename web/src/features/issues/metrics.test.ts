import { describe, expect, it } from "vitest";
import { formatHours, statusSegments, type IssueMetrics } from "./metrics";

describe("formatHours", () => {
  it("picks minutes, hours or days", () => {
    expect([0, 0.005, 0.25, 1, 5.5, 23.99, 24, 27.4, 50, 168, 300].map(formatHours)).toEqual(["<1m", "<1m", "15m", "1h", "5h 30m", "23h 59m", "1d", "1d 3h", "2d 2h", "7d", "12d"]);
    expect(formatHours(-3)).toBe("<1m");
  });
});

const st = (id: string, category: "todo" | "in_progress" | "done") => ({ id, name: id, category });

function metrics(over: Partial<IssueMetrics>): IssueMetrics {
  return { createdAt: "", ageHours: 0, statusSince: "", statusAgeHours: 0, timeInStatus: [], reopened: 0, sprints: 0, ...over };
}

describe("statusSegments", () => {
  it("shares the bar in visit order and textures same-category neighbours", () => {
    const m = metrics({
      statusAgeHours: 2,
      timeInStatus: [
        { status: st("backlog", "todo"), hours: 10, visits: 1 },
        { status: st("selected", "todo"), hours: 6, visits: 1 },
        { status: st("doing", "in_progress"), hours: 2, visits: 2 },
        { status: st("review", "in_progress"), hours: 2, visits: 1 },
      ],
    });
    const { segments, total, resolved } = statusSegments(m, "review");
    expect(total).toBe(20);
    expect(resolved).toBe(false);
    expect(segments.map((s) => [s.status.id, s.share, s.alt, s.current])).toEqual([
      ["backlog", 0.5, false, false],
      ["selected", 0.3, true, false],
      ["doing", 0.1, false, false],
      ["review", 0.1, true, true],
    ]);
  });

  it("stops the bar at resolution but keeps the total for the list", () => {
    const m = metrics({
      statusAgeHours: 100,
      timeInStatus: [
        { status: st("todo", "todo"), hours: 5, visits: 1 },
        { status: st("doing", "in_progress"), hours: 5, visits: 1 },
        { status: st("done", "done"), hours: 100, visits: 1 },
      ],
    });
    const { segments, total, resolved } = statusSegments(m, "done");
    expect(resolved).toBe(true);
    expect(total).toBe(10);
    expect(segments[2]).toMatchObject({ hours: 0, total: 100, share: 0, current: true, alt: false });
  });

  it("counts earlier visits to the current done status (reopened issues)", () => {
    const m = metrics({
      statusAgeHours: 1,
      timeInStatus: [
        { status: st("doing", "in_progress"), hours: 4, visits: 2 },
        { status: st("done", "done"), hours: 5, visits: 2 },
      ],
    });
    const { segments, total } = statusSegments(m, "done");
    expect(total).toBe(8);
    expect(segments[1].hours).toBe(4);
  });

  it("skips empty segments when alternating", () => {
    const m = metrics({
      timeInStatus: [
        { status: st("a", "todo"), hours: 1, visits: 1 },
        { status: st("b", "todo"), hours: 0, visits: 1 },
        { status: st("c", "todo"), hours: 1, visits: 1 },
      ],
    });
    expect(statusSegments(m, "c").segments.map((s) => s.alt)).toEqual([false, false, true]);
    expect(statusSegments(metrics({}), "x")).toEqual({ segments: [], total: 0, resolved: false });
  });
});
