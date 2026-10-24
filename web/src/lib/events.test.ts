import { describe, expect, it } from "vitest";
import { eventsURL } from "./events";

describe("eventsURL", () => {
  it("follows one project, several, or all", () => {
    expect(eventsURL("PLAT")).toBe("/api/v1/events?project=PLAT");
    expect(eventsURL(["OPS", "PLAT", "OPS"])).toBe("/api/v1/events?projects=OPS,PLAT");
    expect(eventsURL("all")).toBe("/api/v1/events?projects=*");
    expect(eventsURL([])).toBeUndefined();
    expect(eventsURL(undefined)).toBeUndefined();
  });
});
