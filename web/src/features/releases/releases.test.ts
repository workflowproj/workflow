import { describe, expect, it } from "vitest";
import { burnupCaption } from "./ReleaseBurnupChart";
import { dateStatus, notesFileName, releaseJql } from "./queries";

describe("dateStatus", () => {
  const today = "2026-10-26";
  it("counts the days left or late of an unreleased release", () => {
    expect(dateStatus({ state: "unreleased", releaseDate: "2026-10-29" }, today)).toEqual({ text: "3 days left", late: false });
    expect(dateStatus({ state: "unreleased", releaseDate: "2026-10-27" }, today)).toEqual({ text: "1 day left", late: false });
    expect(dateStatus({ state: "unreleased", releaseDate: "2026-10-26" }, today)).toEqual({ text: "Due today", late: false });
    expect(dateStatus({ state: "unreleased", releaseDate: "2026-10-25" }, today)).toEqual({ text: "1 day late", late: true });
    expect(dateStatus({ state: "unreleased", releaseDate: "2026-10-24" }, today)).toEqual({ text: "2 days late", late: true });
  });
  it("says nothing once released or without a date", () => {
    expect(dateStatus({ state: "released", releaseDate: "2026-10-20" }, today)).toBeNull();
    expect(dateStatus({ state: "unreleased" }, today)).toBeNull();
  });
});

describe("release notes and queries", () => {
  it("names the notes file after the release", () => {
    expect(notesFileName("3.9.0")).toBe("release-notes-3.9.0.md");
    expect(notesFileName(" Autumn 2026 / beta ")).toBe("release-notes-Autumn-2026-beta.md");
    expect(notesFileName("///")).toBe("release-notes-release.md");
  });
  it("quotes release names in JQL", () => {
    expect(releaseJql("SHOP", "Autumn 2026", true)).toBe('project = SHOP AND fixVersion = "Autumn 2026" AND statusCategory != Done ORDER BY rank');
  });
  it("says where the burnup's scope comes from", () => {
    expect(burnupCaption({ scopeSource: "history" })).toMatch(/fix version changes/);
    expect(burnupCaption({ scopeSource: "current" })).toMatch(/current issues/);
  });
});
