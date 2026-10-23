/*
 * Client-side JQL helpers: a tolerant tokenizer (for highlighting and autocomplete), cursor
 * context analysis, ORDER BY rewriting and the "basic mode" filter <-> JQL mapping. The server
 * (internal/search) remains the authority on what a query means; nothing here throws.
 */

export type TokenKind = "ws" | "lparen" | "rparen" | "comma" | "op" | "string" | "word";

export type Token = {
  kind: TokenKind;
  text: string;
  start: number;
  end: number;
  /** Unquoted value of a string token. */
  value?: string;
  unterminated?: boolean;
};

const SPECIAL = "()=,!<>~\"'";

/** tokenize splits a query like the server's lexer does, but never fails. Offsets are UTF-16 indices. */
export function tokenize(src: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    const start = i;
    if (/\s/.test(c)) {
      while (i < src.length && /\s/.test(src[i])) i++;
      out.push({ kind: "ws", text: src.slice(start, i), start, end: i });
    } else if (c === "(" || c === ")" || c === ",") {
      i++;
      out.push({ kind: c === "(" ? "lparen" : c === ")" ? "rparen" : "comma", text: c, start, end: i });
    } else if (c === '"' || c === "'") {
      i++;
      let value = "";
      let closed = false;
      while (i < src.length) {
        if (src[i] === "\\" && i + 1 < src.length) {
          value += src[i + 1];
          i += 2;
          continue;
        }
        if (src[i] === c) {
          i++;
          closed = true;
          break;
        }
        value += src[i++];
      }
      out.push({ kind: "string", text: src.slice(start, i), start, end: i, value, unterminated: !closed });
    } else if ("=!<>~".includes(c)) {
      i++;
      if (i < src.length && (src[i] === "=" || (c === "!" && src[i] === "~"))) i++;
      out.push({ kind: "op", text: src.slice(start, i), start, end: i });
    } else {
      while (i < src.length && !/\s/.test(src[i]) && !SPECIAL.includes(src[i])) i++;
      out.push({ kind: "word", text: src.slice(start, i), start, end: i });
    }
  }
  return out;
}

/** runeOffsetToIndex converts the server's character (rune) offset into a JS string index. */
export function runeOffsetToIndex(src: string, runes: number): number {
  let idx = 0;
  let n = 0;
  for (const ch of src) {
    if (n >= runes) break;
    idx += ch.length;
    n++;
  }
  return idx;
}

const RESERVED = new Set(["and", "or", "not", "in", "is", "empty", "null", "order", "by", "asc", "desc"]);
const eq = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/** quoteString always renders a quoted JQL string. */
export function quoteString(v: string): string {
  return `"${v.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
}

/** quoteValue renders a value as JQL, quoting it unless it is a plain word, number or function call. */
export function quoteValue(v: string): string {
  if (/^[A-Za-z_][\w]*\(\)$/.test(v)) return v; // currentUser()
  if (v !== "" && /^[\p{L}\p{N}_.@+\-:/]+$/u.test(v) && !RESERVED.has(v.toLowerCase())) return v;
  return quoteString(v);
}

// ---------------------------------------------------------------------------
// Highlighting

export type Highlight = "field" | "keyword" | "operator" | "string" | "function" | "value" | "punct" | "ws";

const LOGICAL = new Set(["and", "or", "not", "order", "by", "asc", "desc", "in", "is", "empty", "null"]);

/** classify gives each token a highlight class for the editor overlay. */
export function classify(tokens: Token[]): Highlight[] {
  const sig = tokens.map((t, i) => ({ t, i })).filter((x) => x.t.kind !== "ws");
  const out: Highlight[] = tokens.map((t) => (t.kind === "ws" ? "ws" : "punct"));
  let expectField = true;
  let inOrder = false;
  for (let k = 0; k < sig.length; k++) {
    const { t, i } = sig[k];
    const next = sig[k + 1]?.t;
    if (t.kind === "op") {
      out[i] = "operator";
      expectField = false;
    } else if (t.kind === "string") {
      out[i] = expectField || inOrder ? "field" : "string";
      expectField = false;
    } else if (t.kind === "word") {
      const w = t.text.toLowerCase();
      if (next?.kind === "lparen" && !LOGICAL.has(w)) {
        out[i] = "function";
      } else if (LOGICAL.has(w) && !(expectField && !["not", "order"].includes(w))) {
        out[i] = "keyword";
        if (w === "and" || w === "or" || w === "not") expectField = true;
        if (w === "by") inOrder = true;
      } else if (expectField || (inOrder && !["asc", "desc"].includes(w))) {
        out[i] = "field";
        expectField = false;
      } else {
        out[i] = "value";
      }
    } else if (t.kind === "lparen") {
      // "(" opens a group where a field is expected, unless it follows IN or a function name.
      const prev = sig[k - 1]?.t;
      expectField = !prev || (prev.kind === "word" && ["and", "or", "not"].includes(prev.text.toLowerCase())) || prev.kind === "lparen";
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Autocomplete context

export type Expect = "field" | "operator" | "value" | "list-value" | "list-next" | "is-value" | "logical" | "by" | "order-field" | "order-dir" | "order-next" | "none";

export type Context = {
  expect: Expect;
  /** Field the clause under the cursor is about (for operator and value suggestions). */
  field?: string;
  /** Operator of that clause ("=", "in", "not in", "is", "is not", or "not" before "in"). */
  op?: string;
  /** Text already typed for the token under the cursor. */
  prefix: string;
  /** Range the accepted suggestion replaces. */
  from: number;
  to: number;
  /** True while no complete condition precedes the cursor. */
  atStart: boolean;
};

/** analyze works out what may be typed at `cursor` and which text a suggestion replaces. */
export function analyze(src: string, cursor: number): Context {
  const all = tokenize(src.slice(0, cursor)).filter((t) => t.kind !== "ws");
  let partial: Token | undefined;
  const last = all[all.length - 1];
  if (last && last.end === cursor) {
    if (last.kind === "word" || (last.kind === "string" && last.unterminated) || (last.kind === "op" && last.text === "!")) partial = last;
  }
  const toks = partial ? all.slice(0, -1) : all;
  // Extend the replaced range over the rest of a word the cursor is inside.
  let to = cursor;
  if (partial?.kind === "word") {
    while (to < src.length && !/\s/.test(src[to]) && !SPECIAL.includes(src[to])) to++;
  }

  let state = "field" as Expect; // widened: done() changes it from a closure
  let field: string | undefined;
  let op: string | undefined;
  let clauses = 0;
  const done = () => {
    state = "logical";
    clauses++;
  };
  for (let k = 0; k < toks.length && state !== "none"; k++) {
    const t = toks[k];
    const w = t.kind === "word" ? t.text.toLowerCase() : "";
    const isValue = t.kind === "word" || t.kind === "string";
    switch (state) {
      case "field":
        if (t.kind === "lparen" || w === "not") break;
        if (w === "order") state = "by";
        else if (isValue) {
          field = t.kind === "string" ? t.value : t.text;
          op = undefined;
          state = "operator";
        } else state = "none";
        break;
      case "operator":
        if (t.kind === "op") {
          op = t.text;
          state = "value";
        } else if (w === "in") {
          op = op === "not" ? "not in" : "in";
          if (toks[k + 1]?.kind === "lparen") {
            k++;
            state = "list-value";
          } else if (toks[k + 1]?.kind === "word" && toks[k + 2]?.kind === "lparen") {
            k = skipCall(toks, k + 1); // sprint in openSprints()
            done();
          } else state = "list-value";
        } else if (w === "not" && op !== "not") op = "not";
        else if (w === "is") {
          op = "is";
          state = "is-value";
        } else state = "none";
        break;
      case "value":
        if (!isValue) state = "none";
        else {
          if (t.kind === "word" && toks[k + 1]?.kind === "lparen") k = skipCall(toks, k);
          done();
        }
        break;
      case "list-value":
        if (!isValue) state = "none";
        else {
          if (t.kind === "word" && toks[k + 1]?.kind === "lparen") k = skipCall(toks, k);
          state = "list-next";
        }
        break;
      case "list-next":
        if (t.kind === "comma") state = "list-value";
        else if (t.kind === "rparen") done();
        else state = "none";
        break;
      case "is-value":
        if (w === "not" && op === "is") op = "is not";
        else if (w === "empty" || w === "null") done();
        else state = "none";
        break;
      case "logical":
        if (w === "and" || w === "or") {
          state = "field";
          field = op = undefined;
        } else if (w === "order") state = "by";
        else if (t.kind !== "rparen") state = "none";
        break;
      case "by":
        state = w === "by" ? "order-field" : "none";
        break;
      case "order-field":
        if (isValue) {
          field = t.kind === "string" ? t.value : t.text;
          state = "order-dir";
        } else state = "none";
        break;
      case "order-dir":
        if (t.kind === "comma") state = "order-field";
        else state = w === "asc" || w === "desc" ? "order-next" : "none";
        break;
      case "order-next":
        state = t.kind === "comma" ? "order-field" : "none";
        break;
    }
  }
  return {
    expect: state,
    field,
    op,
    prefix: partial ? (partial.kind === "string" ? (partial.value ?? "") : partial.text) : "",
    from: partial ? partial.start : cursor,
    to,
    atStart: clauses === 0,
  };
}

function skipCall(toks: Token[], k: number): number {
  // toks[k] is the function name and toks[k+1] its "(": skip to the matching ")".
  let depth = 0;
  for (let j = k + 1; j < toks.length; j++) {
    if (toks[j].kind === "lparen") depth++;
    else if (toks[j].kind === "rparen" && --depth === 0) return j;
  }
  return toks.length - 1;
}

/** applyCompletion replaces the context's range with `insert`, returning the new text and caret. */
export function applyCompletion(src: string, ctx: Pick<Context, "from" | "to">, insert: string, tight = false): { text: string; cursor: number } {
  const after = src.slice(ctx.to);
  const pad = tight || insert.endsWith(" ") || insert.endsWith("(") || after.startsWith(" ") ? "" : " ";
  const text = src.slice(0, ctx.from) + insert + pad + after.replace(/^ (?= )/, "");
  return { text, cursor: ctx.from + insert.length + pad.length };
}

// ---------------------------------------------------------------------------
// ORDER BY

export type SortKey = { field: string; desc: boolean };

/** splitOrderBy separates the conditions from the ORDER BY list (outside strings). */
export function splitOrderBy(jql: string): { where: string; orderBy: string } {
  const toks = tokenize(jql);
  for (let k = 0; k < toks.length; k++) {
    if (toks[k].kind === "word" && eq(toks[k].text, "order")) {
      let j = k + 1;
      while (toks[j]?.kind === "ws") j++;
      if (toks[j]?.kind === "word" && eq(toks[j].text, "by")) {
        return { where: jql.slice(0, toks[k].start).trim(), orderBy: jql.slice(toks[j].end).trim() };
      }
    }
  }
  return { where: jql.trim(), orderBy: "" };
}

export function parseOrderBy(orderBy: string): SortKey[] {
  return orderBy
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean)
    .map((p) => {
      const m = /^("([^"]*)"|\S+)(?:\s+(asc|desc))?$/i.exec(p);
      return m ? { field: m[2] ?? m[1], desc: m[3]?.toLowerCase() === "desc" } : { field: p, desc: false };
    });
}

export function joinJql(where: string, sort: SortKey[]): string {
  const order = sort.map((s) => `${s.field}${s.desc ? " DESC" : " ASC"}`).join(", ");
  return [where.trim(), order ? `ORDER BY ${order}` : ""].filter(Boolean).join(" ");
}

/** currentSort returns the primary sort key, if the query has one. */
export function currentSort(jql: string): SortKey | undefined {
  return parseOrderBy(splitOrderBy(jql).orderBy)[0];
}

const norm = (f: string) => f.toLowerCase().replace(/[\s_-]/g, "");

/**
 * toggleSort makes `field` the primary sort: clicking the current primary flips its direction,
 * otherwise it becomes first (with `defaultDesc`) and earlier keys follow.
 */
export function toggleSort(jql: string, field: string, defaultDesc = false): string {
  const { where, orderBy } = splitOrderBy(jql);
  const keys = parseOrderBy(orderBy);
  const first = keys[0];
  let next: SortKey[];
  if (first && norm(first.field) === norm(field)) next = [{ field: first.field, desc: !first.desc }, ...keys.slice(1)];
  else next = [{ field, desc: defaultDesc }, ...keys.filter((k) => norm(k.field) !== norm(field))].slice(0, 3);
  return joinJql(where, next);
}

// ---------------------------------------------------------------------------
// Basic mode

export const BASIC_FIELDS = ["project", "type", "status", "assignee", "priority", "labels"] as const;
export type BasicField = (typeof BASIC_FIELDS)[number];
export type BasicFilters = Record<BasicField, string[]> & { text: string };

export const emptyBasic = (): BasicFilters => ({ project: [], type: [], status: [], assignee: [], priority: [], labels: [], text: "" });

const fieldAliases: Record<string, BasicField | "text"> = {
  project: "project",
  type: "type",
  issuetype: "type",
  status: "status",
  assignee: "assignee",
  priority: "priority",
  labels: "labels",
  label: "labels",
  text: "text",
};

/** buildJql turns basic filters into JQL; `scopeProject` pins the query to one project. */
export function buildJql(f: BasicFilters, orderBy: string, scopeProject?: string): string {
  const parts: string[] = [];
  if (scopeProject) parts.push(`project = ${quoteValue(scopeProject)}`);
  for (const field of BASIC_FIELDS) {
    if (field === "project" && scopeProject) continue;
    const vs = f[field].map((v) => (v === "EMPTY" ? v : quoteValue(v)));
    if (vs.length === 1) parts.push(`${field} = ${vs[0]}`);
    else if (vs.length > 1) parts.push(`${field} in (${vs.join(", ")})`);
  }
  if (f.text.trim()) parts.push(`text ~ ${quoteString(f.text.trim())}`);
  const where = parts.join(" AND ");
  return [where, orderBy.trim() ? `ORDER BY ${orderBy.trim()}` : ""].filter(Boolean).join(" ");
}

/**
 * parseBasic reads JQL produced by buildJql (or written in the same shape: clauses joined by
 * AND, each `field = v`, `field in (...)` or `text ~ "..."`). It returns null when the query
 * cannot be shown as basic filters.
 */
export function parseBasic(jql: string, scopeProject?: string): { filters: BasicFilters; orderBy: string } | null {
  const { where, orderBy } = splitOrderBy(jql);
  const toks = tokenize(where).filter((t) => t.kind !== "ws");
  if (toks.some((t) => t.unterminated)) return null;
  const filters = emptyBasic();
  const seen = new Set<string>();
  let scoped = !scopeProject;
  let k = 0;
  const value = (): string | null => {
    const t = toks[k];
    if (!t) return null;
    if (t.kind === "string") {
      k++;
      return t.value ?? "";
    }
    if (t.kind !== "word" || ["and", "or", "not", "in", "is", "order"].includes(t.text.toLowerCase())) return null;
    k++;
    if (toks[k]?.kind === "lparen") {
      if (toks[k + 1]?.kind !== "rparen") return null;
      k += 2;
      return t.text.toLowerCase() === "currentuser" ? "currentUser()" : `${t.text}()`;
    }
    return t.text.toLowerCase() === "null" ? "EMPTY" : t.text.toLowerCase() === "empty" ? "EMPTY" : t.text;
  };
  while (k < toks.length) {
    if (k > 0) {
      if (toks[k].kind !== "word" || !eq(toks[k].text, "and")) return null;
      k++;
    }
    const ft = toks[k++];
    if (!ft || (ft.kind !== "word" && ft.kind !== "string")) return null;
    const field = fieldAliases[norm(ft.kind === "string" ? (ft.value ?? "") : ft.text)];
    if (!field || seen.has(field)) return null;
    seen.add(field);
    const op = toks[k++];
    if (!op) return null;
    let values: string[] = [];
    if (field === "text") {
      if (op.kind !== "op" || op.text !== "~") return null;
      const v = value();
      if (v === null) return null;
      filters.text = v;
      continue;
    }
    if (op.kind === "op" && op.text === "=") {
      const v = value();
      if (v === null) return null;
      values = [v];
    } else if (op.kind === "word" && eq(op.text, "in")) {
      if (toks[k++]?.kind !== "lparen") return null;
      for (;;) {
        const v = value();
        if (v === null) return null;
        values.push(v);
        const sep = toks[k++];
        if (sep?.kind === "rparen") break;
        if (sep?.kind !== "comma") return null;
      }
    } else return null;
    if (field === "project" && scopeProject) {
      if (values.length !== 1 || !eq(values[0], scopeProject)) return null;
      scoped = true;
      continue;
    }
    filters[field] = values;
  }
  if (!scoped) return null;
  return { filters, orderBy };
}

export function basicIsEmpty(f: BasicFilters): boolean {
  return BASIC_FIELDS.every((k) => f[k].length === 0) && !f.text.trim();
}
