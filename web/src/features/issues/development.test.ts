import { describe, expect, it } from "vitest";
import { showDevelopment } from "./development";

describe("showDevelopment", () => {
  it("always shows the section in software projects", () => {
    expect(showDevelopment({ service: false, repositories: 0, linked: 0 })).toBe(true);
    expect(showDevelopment({ service: false, repositories: undefined, linked: 0 })).toBe(true);
  });

  it("hides it on service requests with no repository and nothing linked", () => {
    expect(showDevelopment({ service: true, repositories: 0, linked: 0 })).toBe(false);
    expect(showDevelopment({ service: true, repositories: undefined, linked: 0 })).toBe(false);
    expect(showDevelopment({ service: true, repositories: 1, linked: 0 })).toBe(true);
    expect(showDevelopment({ service: true, repositories: 0, linked: 2 })).toBe(true);
  });
});
