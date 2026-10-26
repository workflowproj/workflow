import { describe, expect, it } from "vitest";
import { plural } from "./format";

describe("plural", () => {
  it("uses the singular for one and the plural otherwise", () => {
    expect(plural(1, "group")).toBe("1 group");
    expect(plural(0, "group")).toBe("0 groups");
    expect(plural(2, "group")).toBe("2 groups");
  });
  it("takes an irregular plural", () => {
    expect(plural(1, "person", "people")).toBe("1 person");
    expect(plural(3, "person", "people")).toBe("3 people");
    expect(plural(1, "kind of request", "kinds of request")).toBe("1 kind of request");
  });
});
