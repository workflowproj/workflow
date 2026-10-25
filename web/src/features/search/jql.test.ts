import { describe, expect, it } from "vitest";
import { analyze, applyCompletion, buildJql, classify, currentSort, emptyBasic, parseBasic, quoteValue, runeOffsetToIndex, splitOrderBy, toggleSort, tokenize } from "./jql";

describe("tokenize", () => {
  it("splits words, operators, strings and punctuation with offsets", () => {
    const toks = tokenize('status in ("To Do", Done) AND x != 3').filter((t) => t.kind !== "ws");
    expect(toks.map((t) => [t.kind, t.text])).toEqual([
      ["word", "status"],
      ["word", "in"],
      ["lparen", "("],
      ["string", '"To Do"'],
      ["comma", ","],
      ["word", "Done"],
      ["rparen", ")"],
      ["word", "AND"],
      ["word", "x"],
      ["op", "!="],
      ["word", "3"],
    ]);
    expect(toks[3].value).toBe("To Do");
    expect(toks[3].start).toBe(11);
  });

  it("keeps unterminated strings and escapes", () => {
    const [t] = tokenize('"say \\"hi');
    expect(t.unterminated).toBe(true);
    expect(t.value).toBe('say "hi');
  });

  it("covers the whole input", () => {
    const src = 'a=b  ORDER BY "x"';
    expect(
      tokenize(src)
        .map((t) => t.text)
        .join(""),
    ).toBe(src);
  });
});

describe("classify", () => {
  it("marks fields, operators, values, keywords and functions", () => {
    const src = "assignee = currentUser() AND status != Done ORDER BY updated DESC";
    const toks = tokenize(src);
    const cls = classify(toks);
    const pick = (text: string) => cls[toks.findIndex((t) => t.text === text)];
    expect(pick("assignee")).toBe("field");
    expect(pick("=")).toBe("operator");
    expect(pick("currentUser")).toBe("function");
    expect(pick("AND")).toBe("keyword");
    expect(pick("status")).toBe("field");
    expect(pick("Done")).toBe("value");
    expect(pick("ORDER")).toBe("keyword");
    expect(pick("updated")).toBe("field");
    expect(pick("DESC")).toBe("keyword");
  });
});

describe("analyze", () => {
  const at = (src: string) => analyze(src, src.length);

  it("expects a field at the start and while typing one", () => {
    expect(at("")).toMatchObject({ expect: "field", prefix: "", atStart: true });
    expect(at("sta")).toMatchObject({ expect: "field", prefix: "sta", from: 0 });
    expect(at("status = Done AND pri")).toMatchObject({ expect: "field", prefix: "pri", from: 18, atStart: false });
    expect(at("(status = Done OR ")).toMatchObject({ expect: "field" });
    expect(at("NOT ")).toMatchObject({ expect: "field" });
  });

  it("expects an operator after a field", () => {
    expect(at("status ")).toMatchObject({ expect: "operator", field: "status" });
    expect(at("status !")).toMatchObject({ expect: "operator", prefix: "!" });
    expect(at("status not ")).toMatchObject({ expect: "operator", op: "not" });
  });

  it("expects values after an operator, inside lists and in strings", () => {
    expect(at("status = ")).toMatchObject({ expect: "value", field: "status", op: "=" });
    expect(at("status = In")).toMatchObject({ expect: "value", prefix: "In", from: 9 });
    expect(at('status = "In P')).toMatchObject({ expect: "value", prefix: "In P", from: 9 });
    expect(at("priority in (High, ")).toMatchObject({ expect: "list-value", field: "priority", op: "in" });
    expect(at("priority not in (")).toMatchObject({ expect: "list-value", op: "not in" });
    expect(at("priority in (High")).toMatchObject({ expect: "list-value", prefix: "High" });
    expect(at("priority in (High ")).toMatchObject({ expect: "list-next" });
    expect(at("assignee is ")).toMatchObject({ expect: "is-value", op: "is" });
    expect(at("assignee is not ")).toMatchObject({ expect: "is-value", op: "is not" });
  });

  it("expects AND/OR/ORDER BY after a complete condition", () => {
    expect(at("status = Done ")).toMatchObject({ expect: "logical", atStart: false });
    expect(at("status = Done a")).toMatchObject({ expect: "logical", prefix: "a" });
    expect(at("assignee = currentUser() ")).toMatchObject({ expect: "logical" });
    expect(at("sprint in openSprints() ")).toMatchObject({ expect: "logical" });
    expect(at("priority in (High, Low) ")).toMatchObject({ expect: "logical" });
    expect(at("assignee is EMPTY ")).toMatchObject({ expect: "logical" });
    expect(at('text ~ "login" ')).toMatchObject({ expect: "logical" });
  });

  it("handles ORDER BY", () => {
    expect(at("ORDER ")).toMatchObject({ expect: "by" });
    expect(at("status = Done ORDER BY ")).toMatchObject({ expect: "order-field" });
    expect(at("ORDER BY upd")).toMatchObject({ expect: "order-field", prefix: "upd" });
    expect(at("ORDER BY updated ")).toMatchObject({ expect: "order-dir", field: "updated" });
    expect(at("ORDER BY updated DESC, ")).toMatchObject({ expect: "order-field" });
  });

  it("replaces the whole word under the cursor", () => {
    const src = "status = Done";
    const ctx = analyze(src, 2);
    expect(ctx).toMatchObject({ expect: "field", prefix: "st", from: 0, to: 6 });
    expect(applyCompletion(src, ctx, "statusCategory")).toEqual({ text: "statusCategory = Done", cursor: 14 });
  });

  it("adds a space after a completion unless one follows", () => {
    expect(applyCompletion("sta", { from: 0, to: 3 }, "status")).toEqual({ text: "status ", cursor: 7 });
    expect(applyCompletion("status in", { from: 7, to: 9 }, "in (")).toEqual({ text: "status in (", cursor: 11 });
  });
});

describe("quoting and positions", () => {
  it("quotes values only when needed", () => {
    expect(quoteValue("Done")).toBe("Done");
    expect(quoteValue("ada@example.com")).toBe("ada@example.com");
    expect(quoteValue("-7d")).toBe("-7d");
    expect(quoteValue("In Progress")).toBe('"In Progress"');
    expect(quoteValue("Won't Do")).toBe('"Won\'t Do"');
    expect(quoteValue("and")).toBe('"and"');
    expect(quoteValue('say "hi"')).toBe('"say \\"hi\\""');
    expect(quoteValue("currentUser()")).toBe("currentUser()");
  });

  it("converts rune offsets to string indices", () => {
    expect(runeOffsetToIndex("abc", 2)).toBe(2);
    expect(runeOffsetToIndex('text ~ "😀" AND x', 11)).toBe(12);
  });
});

describe("ORDER BY", () => {
  it("splits outside strings", () => {
    expect(splitOrderBy('summary ~ "order by" ORDER BY created DESC')).toEqual({ where: 'summary ~ "order by"', orderBy: "created DESC" });
    expect(splitOrderBy("project = PLAT")).toEqual({ where: "project = PLAT", orderBy: "" });
  });

  it("toggles and prepends sort keys", () => {
    expect(toggleSort("project = PLAT ORDER BY updated DESC", "updated")).toBe("project = PLAT ORDER BY updated ASC");
    expect(toggleSort("project = PLAT ORDER BY updated DESC", "priority", true)).toBe("project = PLAT ORDER BY priority DESC, updated DESC");
    expect(toggleSort("", "key")).toBe("ORDER BY key ASC");
    expect(currentSort("x = y order by Priority desc, key")).toEqual({ field: "Priority", desc: true });
  });
});

describe("basic mode", () => {
  it("builds JQL from filters", () => {
    const f = { ...emptyBasic(), project: ["PLAT"], status: ["To Do", "In Progress"], assignee: ["currentUser()", "EMPTY"], text: 'card "amex"' };
    expect(buildJql(f, "updated DESC")).toBe(
      'project = PLAT AND status in ("To Do", "In Progress") AND assignee in (currentUser(), EMPTY) AND text ~ "card \\"amex\\"" ORDER BY updated DESC',
    );
    expect(buildJql(emptyBasic(), "")).toBe("");
    expect(buildJql({ ...emptyBasic(), priority: ["High"] }, "", "OPS")).toBe("project = OPS AND priority = High");
  });

  it("round-trips through parseBasic", () => {
    const f = { ...emptyBasic(), type: ["Bug"], labels: ["web", "needs review"], assignee: ["bob@example.com"], text: "login" };
    const jql = buildJql(f, "created DESC");
    expect(parseBasic(jql)).toEqual({ filters: f, orderBy: "created DESC" });
    const scoped = buildJql(f, "", "PLAT");
    expect(parseBasic(scoped, "PLAT")).toEqual({ filters: f, orderBy: "" });
  });

  it("accepts hand-written equivalents", () => {
    expect(parseBasic("Status = Done and issuetype IN (Bug,Task) order by key")?.filters).toMatchObject({ status: ["Done"], type: ["Bug", "Task"] });
    expect(parseBasic("assignee is EMPTY")).toBeNull();
  });

  it("rejects queries basic mode cannot show", () => {
    expect(parseBasic("status = Done OR status = Open")).toBeNull();
    expect(parseBasic("status != Done")).toBeNull();
    expect(parseBasic("created >= -7d")).toBeNull();
    expect(parseBasic("status = Done AND status = Open")).toBeNull();
    expect(parseBasic("NOT status = Done")).toBeNull();
    expect(parseBasic("status = Done", "PLAT")).toBeNull();
    expect(parseBasic("project = OPS", "PLAT")).toBeNull();
    expect(parseBasic('text ~ "unterminated')).toBeNull();
  });
});
