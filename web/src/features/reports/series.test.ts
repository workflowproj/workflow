import { describe, expect, it } from "vitest";
import { arcPath, foldGroups, foldSeries, stackSeries } from "./series";

describe("foldSeries", () => {
  const s = (key: string, counts: number[]) => ({ key, label: key.toUpperCase(), category: "todo", counts });
  it("keeps short lists as they are", () => {
    const list = [s("a", [1]), s("b", [2])];
    expect(foldSeries(list, 3)).toBe(list);
  });
  it("folds the tail into Other", () => {
    const out = foldSeries([s("a", [1, 1]), s("b", [2, 0]), s("c", [3, 1]), s("d", [4, 2])], 3);
    expect(out.map((x) => x.label)).toEqual(["A", "B", "Other"]);
    expect(out[2]).toMatchObject({ key: "", counts: [7, 3] });
  });
});

describe("stackSeries", () => {
  it("stacks bands on each other", () => {
    expect(stackSeries([[1, 2], [3, 0]])).toEqual([
      { lower: [0, 0], upper: [1, 2] },
      { lower: [1, 2], upper: [4, 2] },
    ]);
    expect(stackSeries([])).toEqual([]);
  });
});

describe("foldGroups", () => {
  const g = (key: string, value: number) => ({ key, label: key || "None", value });
  it("keeps None last and folds the smallest named groups", () => {
    const out = foldGroups([g("a", 5), g("b", 4), g("c", 3), g("d", 2), g("", 6)], 4);
    expect(out.map((x) => x.label)).toEqual(["a", "b", "2 more", "None"]);
    expect(out[2].value).toBe(5);
  });
  it("leaves a short list alone", () => {
    expect(foldGroups([g("a", 1), g("", 2)], 4).map((x) => x.key)).toEqual(["a", ""]);
  });
});

describe("arcPath", () => {
  it("draws a quarter segment", () => {
    expect(arcPath(10, 10, 10, 5, 0, Math.PI / 2)).toBe("M10,0A10,10 0 0 1 20,10L15,10A5,5 0 0 0 10,5Z");
  });
  it("splits a full ring into two arcs", () => {
    expect(arcPath(0, 0, 10, 5, 0, Math.PI * 2).match(/M/g)).toHaveLength(2);
  });
});
