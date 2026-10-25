import { describe, expect, it } from "vitest";
import { hasFilters, jqlString, NO_FILTERS, quickFilterJql } from "./jql";

describe("quickFilterJql", () => {
  it("is empty without filters", () => {
    expect(quickFilterJql(NO_FILTERS)).toBe("");
    expect(hasFilters(NO_FILTERS)).toBe(false);
  });
  it("combines the quick filters", () => {
    expect(quickFilterJql({ text: " pay ", mine: true, recent: true })).toBe('assignee = currentUser() AND updated >= -1d AND text ~ "pay"');
  });
  it("matches issue keys exactly or as text", () => {
    expect(quickFilterJql({ ...NO_FILTERS, text: "plat-4" })).toBe('(key = PLAT-4 OR text ~ "plat-4")');
  });
  it("escapes quotes", () => {
    expect(jqlString('say "hi" \\o/')).toBe('"say \\"hi\\" \\\\o/"');
  });
});
