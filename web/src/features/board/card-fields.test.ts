import { describe, expect, it } from "vitest";
import { ageDots, cardLayout, daysLabel, daysSince, daysTitle, DEFAULT_CARD_FIELDS, dueLabel, localDate, moveCardField, normalizeCardFields, toggleCardField } from "./card-fields";

describe("normalizeCardFields", () => {
  it("defaults a missing list and drops unknown or repeated fields", () => {
    expect(normalizeCardFields(undefined)).toEqual(DEFAULT_CARD_FIELDS);
    expect(normalizeCardFields([])).toEqual([]);
    expect(normalizeCardFields(["points", "colour", "points", "dueDate"])).toEqual(["points", "dueDate"]);
  });
});

describe("toggle and move", () => {
  it("appends a shown field and removes a hidden one", () => {
    expect(toggleCardField(["epic", "points"], "dueDate", true)).toEqual(["epic", "points", "dueDate"]);
    expect(toggleCardField(["epic", "points"], "epic", false)).toEqual(["points"]);
    expect(toggleCardField(["epic", "points"], "epic", true)).toEqual(["points", "epic"]);
  });
  it("moves within bounds", () => {
    expect(moveCardField(["a", "b", "c"] as never, "c" as never, 0)).toEqual(["c", "a", "b"]);
    expect(moveCardField(["epic", "labels", "points"], "epic", 9)).toEqual(["labels", "points", "epic"]);
    expect(moveCardField(["epic"], "points", 0)).toEqual(["epic"]);
  });
});

describe("cardLayout", () => {
  it("keeps the default card: tags above, points and assignee at the right", () => {
    expect(cardLayout(DEFAULT_CARD_FIELDS)).toEqual({ tags: ["epic", "labels"], left: ["priority", "dev"], right: ["points", "assignee"] });
  });
  it("only pins a trailing run of points and assignee", () => {
    expect(cardLayout(["assignee", "priority", "dueDate"])).toEqual({ tags: [], left: ["assignee", "priority", "dueDate"], right: [] });
    expect(cardLayout(["labels", "points", "daysInColumn", "assignee", "epic"])).toEqual({ tags: ["labels", "epic"], left: ["points", "daysInColumn"], right: ["assignee"] });
    expect(cardLayout([])).toEqual({ tags: [], left: [], right: [] });
  });
});

describe("days in column", () => {
  const now = Date.parse("2026-10-24T12:00:00Z");
  it("counts whole days", () => {
    expect(daysSince(undefined, now)).toBeUndefined();
    expect(daysSince("nonsense", now)).toBeUndefined();
    expect(daysSince("2026-10-24T09:00:00Z", now)).toBe(0);
    expect(daysSince("2026-10-21T12:00:00Z", now)).toBe(3);
    expect(daysSince("2026-10-25T12:00:00Z", now)).toBe(0);
  });
  it("labels and scales", () => {
    expect(daysLabel(0)).toBe("<1d");
    expect(daysLabel(3)).toBe("3d");
    expect([0, 1, 2, 3, 6, 7, 13, 14, 90].map(ageDots)).toEqual([0, 1, 1, 2, 2, 3, 3, 4, 4]);
    expect(daysTitle("Code review", 3)).toBe("In Code review for 3 days");
    expect(daysTitle("Code review", 1)).toBe("In Code review for 1 day");
    expect(daysTitle("To Do", 0)).toBe("In To Do for less than a day");
    expect(daysTitle("In Progress", 2)).toBe("In Progress for 2 days");
  });
});

describe("dueLabel", () => {
  const now = new Date(2026, 9, 24, 15, 0);
  it("formats short dates, adding the year when it differs", () => {
    expect(dueLabel("2026-10-30", now, false, "en-US")).toEqual({ text: "Oct 30", overdue: false, title: "Due Oct 30, 2026" });
    expect(dueLabel("2027-01-02", now, false, "en-US").text).toBe("Jan 2, 2027");
  });
  it("is overdue after the day passes, unless done", () => {
    expect(dueLabel("2026-10-24", now, false, "en-US").overdue).toBe(false);
    expect(dueLabel("2026-10-23", now, false, "en-US")).toMatchObject({ overdue: true, title: "Overdue: due Oct 23, 2026" });
    expect(dueLabel("2026-10-23", now, true, "en-US").overdue).toBe(false);
  });
  it("uses the local calendar day", () => {
    expect(localDate(new Date(2026, 0, 5, 23, 59))).toBe("2026-01-05");
  });
});
