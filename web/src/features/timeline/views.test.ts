import { describe, expect, it } from "vitest";
import { canChangeView, pickView, splitKeys, type TimelineView } from "./views";

const view = (id: string, over: Partial<TimelineView> = {}): TimelineView => ({
  id,
  name: id,
  description: "",
  shared: false,
  mine: true,
  owner: { id: "u1", displayName: "Ada" } as TimelineView["owner"],
  projects: [],
  hiddenProjects: 0,
  ...over,
});

describe("pickView", () => {
  const shared = view("shared", { mine: false, shared: true });
  const own = view("own");
  const other = view("other");

  it("opens the timeline last used when it is still there", () => {
    expect(pickView([shared, own, other], "other")?.id).toBe("other");
  });
  it("falls back to your first own timeline, before shared ones", () => {
    expect(pickView([shared, own, other], "deleted")?.id).toBe("own");
    expect(pickView([shared, own])?.id).toBe("own");
  });
  it("uses a shared timeline when you have none of your own", () => {
    expect(pickView([shared])?.id).toBe("shared");
  });
  it("has nothing to open without timelines", () => {
    expect(pickView([], "x")).toBeUndefined();
  });
});

describe("canChangeView", () => {
  it("lets the owner change their timeline", () => {
    expect(canChangeView(view("a"), false)).toBe(true);
  });
  it("lets an org admin change a shared timeline, not a private one", () => {
    expect(canChangeView(view("a", { mine: false, shared: true }), true)).toBe(true);
    expect(canChangeView(view("a", { mine: false, shared: false }), true)).toBe(false);
    expect(canChangeView(view("a", { mine: false, shared: true }), false)).toBe(false);
  });
});

describe("splitKeys", () => {
  it("reads a comma list and ignores blanks", () => {
    expect(splitKeys(" APP, ,API,")).toEqual(["APP", "API"]);
    expect(splitKeys(undefined)).toEqual([]);
  });
});
