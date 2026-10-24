import { describe, expect, it } from "vitest";
import type { Issue, User } from "@/api/types";
import { filterBacklog, filterColumns, matchesPeople, peopleOf, UNASSIGNED } from "./people";

const user = (id: string, displayName: string) => ({ id, displayName }) as User;
const ada = user("u-ada", "Ada");
const bob = user("u-bob", "Bob");
const cy = user("u-cy", "Cy");
const issue = (id: string, assignee?: User) => ({ id, key: `P-${id}`, assignee }) as Issue;

describe("peopleOf", () => {
  const issues = [issue("1", bob), issue("2", cy), issue("3", cy), issue("4"), issue("5", ada)];

  it("puts you first, then by issue count and name, and Unassigned last", () => {
    expect(peopleOf(issues, "u-ada").map((p) => [p.id, p.count])).toEqual([
      ["u-ada", 1],
      ["u-cy", 2],
      ["u-bob", 1],
      [UNASSIGNED, 1],
    ]);
  });

  it("keeps selected people who no longer have issues", () => {
    const known = new Map([[bob.id, bob]]);
    const people = peopleOf([issue("2", cy)], undefined, [bob.id, "u-gone"], known);
    expect(people.map((p) => [p.id, p.count])).toEqual([
      ["u-cy", 1],
      ["u-bob", 0],
    ]);
    expect(peopleOf([], undefined, [UNASSIGNED]).map((p) => p.id)).toEqual([UNASSIGNED]);
  });
});

describe("people filtering", () => {
  it("matches everyone when nobody is selected", () => {
    expect(matchesPeople(issue("1"), [])).toBe(true);
    expect(matchesPeople(issue("1", ada), [bob.id])).toBe(false);
    expect(matchesPeople(issue("1"), [UNASSIGNED])).toBe(true);
  });

  it("filters board columns but keeps their full count", () => {
    const columns = [{ id: "todo", issues: [issue("1", ada), issue("2", bob), issue("3")] }];
    expect(filterColumns(columns, [])).toBe(columns);
    const [col] = filterColumns(columns, [ada.id, UNASSIGNED]);
    expect(col.issues.map((i) => i.id)).toEqual(["1", "3"]);
    expect(col.total).toBe(3);
  });

  it("filters the backlog and its sprints", () => {
    const data = { sprints: [{ sprint: { id: "s1" }, issues: [issue("1", ada), issue("2", bob)] }], backlog: [issue("3", bob), issue("4", ada)] };
    const out = filterBacklog(data, [bob.id]);
    expect(out.sprints[0].issues.map((i) => i.id)).toEqual(["2"]);
    expect(out.backlog.map((i) => i.id)).toEqual(["3"]);
    expect(out.sprints[0].sprint).toBe(data.sprints[0].sprint);
  });
});
