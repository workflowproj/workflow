import { describe, expect, it } from "vitest";
import { linkIssueKeys, projectKeySet } from "./issue-keys";

const keys = projectKeySet([{ key: "PLAT", keyAliases: ["PLATFORM"] }, { key: "OPS" }]);

describe("linkIssueKeys", () => {
  it("links keys of known projects, including earlier keys", () => {
    expect(linkIssueKeys("Fixed in PLAT-42 and OPS-7.", keys)).toBe("Fixed in [PLAT-42](/browse/PLAT-42) and [OPS-7](/browse/OPS-7).");
    expect(linkIssueKeys("See PLATFORM-3", keys)).toBe("See [PLATFORM-3](/browse/PLATFORM-3)");
  });

  it("leaves look-alikes of other projects as text", () => {
    expect(linkIssueKeys("Due by DS-2026 under ISO-9001, see PLAT-1", keys)).toBe("Due by DS-2026 under ISO-9001, see [PLAT-1](/browse/PLAT-1)");
  });

  it("skips code, existing links and URLs", () => {
    const md = "`PLAT-1` [PLAT-2](https://x.test) https://x.test/PLAT-3 ```\nPLAT-4\n``` PLAT-5";
    expect(linkIssueKeys(md, keys)).toBe("`PLAT-1` [PLAT-2](https://x.test) https://x.test/PLAT-3 ```\nPLAT-4\n``` [PLAT-5](/browse/PLAT-5)");
  });

  it("links nothing until the projects are known", () => {
    expect(linkIssueKeys("PLAT-1", new Set())).toBe("PLAT-1");
  });
});
