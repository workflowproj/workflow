import { Document as YamlDocument, isMap, isScalar, isSeq, parseDocument, visit } from "yaml";

/*
 * The authored workflow document, mirroring internal/workflow/definition.go (Document). The
 * editor works on this model; the canvas and the YAML tab are two views of it.
 */

export type Category = "todo" | "in_progress" | "done";
export const CATEGORIES: Category[] = ["todo", "in_progress", "done"];

export interface Position {
  x: number;
  y: number;
}
export interface StatusDoc {
  name: string;
  category: Category;
  initial?: boolean;
}
export interface ValidatorDoc {
  expr: string;
  message?: string;
}
export interface ScreenFieldDoc {
  field: string;
  required?: boolean;
  /** A resolution field may offer its own resolutions (the risk workflow's Mitigated, Accepted…). */
  options?: string[];
}
/** A post-function sets exactly one of these keys. */
export interface PostFunctionDoc {
  assignTo?: string;
  setField?: { field: string; value?: unknown };
  clearField?: string;
  notify?: string;
  webhook?: string;
}
export type PostFunctionKind = keyof PostFunctionDoc;
export const POST_FUNCTION_KINDS: PostFunctionKind[] = ["assignTo", "setField", "clearField", "notify", "webhook"];

export interface TransitionDoc {
  id?: string;
  name: string;
  /** Source statuses; undefined means "from any status" (a global transition). */
  from?: string[];
  to: string;
  condition?: string;
  validators?: ValidatorDoc[];
  screen?: ScreenFieldDoc[];
  postFunctions?: PostFunctionDoc[];
}
export interface WorkflowDoc {
  name: string;
  description?: string;
  statuses: StatusDoc[];
  transitions: TransitionDoc[];
  layout?: Record<string, Position>;
}

const same = (a: string, b: string) => a.trim().toLowerCase() === b.trim().toLowerCase();

/** slug mirrors workflow.Slug: "In Review" -> "in-review" (transition ids, smart commits). */
export function slug(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export function postFunctionKind(pf: PostFunctionDoc): PostFunctionKind | undefined {
  return POST_FUNCTION_KINDS.find((k) => pf[k] !== undefined);
}

// ---------------------------------------------------------------------------
// Parsing

export type ParseResult = { ok: true; doc: WorkflowDoc } | { ok: false; error: string; line?: number };

function str(v: unknown): string {
  return v === undefined || v === null ? "" : String(v);
}

/** normalize turns a loosely-typed parsed document into a WorkflowDoc, accepting the same short forms as the server. */
export function normalize(raw: unknown): WorkflowDoc {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw new Error("The document must be a mapping with name, statuses and transitions");
  const r = raw as Record<string, unknown>;
  const statusesRaw = r.statuses ?? [];
  const transitionsRaw = r.transitions ?? [];
  if (!Array.isArray(statusesRaw)) throw new Error("statuses must be a list");
  if (!Array.isArray(transitionsRaw)) throw new Error("transitions must be a list");

  const statuses: StatusDoc[] = statusesRaw.map((s, i) => {
    if (!s || typeof s !== "object") throw new Error(`statuses[${i}] must be a mapping`);
    const o = s as Record<string, unknown>;
    const st: StatusDoc = { name: str(o.name), category: (str(o.category) || "todo") as Category };
    if (o.initial === true) st.initial = true;
    return st;
  });

  const transitions: TransitionDoc[] = transitionsRaw.map((t, i) => {
    if (!t || typeof t !== "object") throw new Error(`transitions[${i}] must be a mapping`);
    const o = t as Record<string, unknown>;
    const tr: TransitionDoc = { name: str(o.name), to: str(o.to) };
    if (o.id !== undefined && o.id !== null && str(o.id) !== "") tr.id = str(o.id);
    const from = o.from === undefined || o.from === null ? [] : Array.isArray(o.from) ? o.from.map(str) : [str(o.from)];
    if (from.length > 0 && !from.includes("*")) tr.from = from;
    if (o.condition) tr.condition = str(o.condition);
    if (Array.isArray(o.validators) && o.validators.length) {
      tr.validators = o.validators.map((v) => {
        if (typeof v === "string") return { expr: v };
        const vo = (v ?? {}) as Record<string, unknown>;
        const out: ValidatorDoc = { expr: str(vo.expr) };
        if (vo.message) out.message = str(vo.message);
        return out;
      });
    }
    if (Array.isArray(o.screen) && o.screen.length) {
      tr.screen = o.screen.map((f) => {
        if (typeof f === "string") return { field: f };
        const fo = (f ?? {}) as Record<string, unknown>;
        const out: ScreenFieldDoc = { field: str(fo.field) };
        if (fo.required === true) out.required = true;
        if (Array.isArray(fo.options) && fo.options.length) out.options = fo.options.map(str);
        return out;
      });
    }
    if (Array.isArray(o.postFunctions) && o.postFunctions.length) {
      tr.postFunctions = o.postFunctions.map((p) => {
        const po = (p ?? {}) as Record<string, unknown>;
        const out: PostFunctionDoc = {};
        if (po.assignTo !== undefined) out.assignTo = str(po.assignTo);
        if (po.setField !== undefined) {
          const sf = (po.setField ?? {}) as Record<string, unknown>;
          out.setField = "value" in sf ? { field: str(sf.field), value: sf.value } : { field: str(sf.field) };
        }
        if (po.clearField !== undefined) out.clearField = str(po.clearField);
        if (po.notify !== undefined) out.notify = str(po.notify);
        if (po.webhook !== undefined) out.webhook = str(po.webhook);
        return out;
      });
    }
    return tr;
  });

  const doc: WorkflowDoc = { name: str(r.name), statuses, transitions };
  if (r.description) doc.description = str(r.description);
  if (r.layout && typeof r.layout === "object" && !Array.isArray(r.layout)) {
    const layout: Record<string, Position> = {};
    for (const [k, v] of Object.entries(r.layout as Record<string, unknown>)) {
      const p = (v ?? {}) as Record<string, unknown>;
      const x = Number(p.x);
      const y = Number(p.y);
      if (Number.isFinite(x) && Number.isFinite(y)) layout[k] = { x, y };
    }
    doc.layout = layout;
  }
  return doc;
}

/** parseWorkflowYaml parses YAML 1.2 (like the server) into a WorkflowDoc, reporting the first error. */
export function parseWorkflowYaml(text: string): ParseResult {
  const d = parseDocument(text, { version: "1.2", prettyErrors: true });
  if (d.errors.length) {
    const e = d.errors[0];
    return { ok: false, error: e.message.split("\n")[0], line: e.linePos?.[0]?.line };
  }
  const raw = d.toJS();
  if (raw === null || raw === undefined) return { ok: false, error: "The document is empty" };
  try {
    return { ok: true, doc: normalize(raw) };
  } catch (e) {
    return { ok: false, error: (e as Error).message };
  }
}

/** fromDefinition converts a compiled definition (status ids) back into a document, like workflow.Decompile. */
export function fromDefinition(def: {
  name: string;
  description?: string;
  statuses: { id: string; name: string; category: Category; initial: boolean }[];
  transitions: { id: string; name: string; from?: string[]; to: string; condition?: string; validators?: ValidatorDoc[]; screen?: ScreenFieldDoc[]; postFunctions?: PostFunctionDoc[] }[];
  layout?: Record<string, Position>;
}): WorkflowDoc {
  const names = new Map(def.statuses.map((s) => [s.id, s.name]));
  return normalize({
    name: def.name,
    description: def.description,
    statuses: def.statuses.map((s) => ({ name: s.name, category: s.category, initial: s.initial || undefined })),
    transitions: def.transitions.map((t) => ({ ...t, from: t.from?.map((f) => names.get(f) ?? f), to: names.get(t.to) ?? t.to })),
    layout: def.layout,
  });
}

// ---------------------------------------------------------------------------
// YAML output

function cleanTransition(t: TransitionDoc): Record<string, unknown> {
  const o: Record<string, unknown> = {};
  if (t.id) o.id = t.id;
  o.name = t.name;
  if (t.from && t.from.length) o.from = [...t.from];
  o.to = t.to;
  if (t.condition?.trim()) o.condition = t.condition.trim();
  if (t.validators?.length) o.validators = t.validators.map((v) => (v.message ? { expr: v.expr, message: v.message } : { expr: v.expr }));
  if (t.screen?.length)
    o.screen = t.screen.map((f) => ({ field: f.field, ...(f.required ? { required: true } : {}), ...(f.options?.some(Boolean) ? { options: f.options.filter(Boolean) } : {}) }));
  if (t.postFunctions?.length) {
    o.postFunctions = t.postFunctions.map((p) => {
      const k = postFunctionKind(p);
      if (!k) return {};
      if (k === "setField") {
        const sf = p.setField!;
        return { setField: sf.value === undefined ? { field: sf.field } : { field: sf.field, value: sf.value } };
      }
      return { [k]: p[k] };
    });
  }
  return o;
}

/** toPlain produces the object that is serialised, with keys in the server's order. */
export function toPlain(doc: WorkflowDoc): Record<string, unknown> {
  const out: Record<string, unknown> = { kind: "Workflow", name: doc.name };
  if (doc.description) out.description = doc.description;
  out.statuses = doc.statuses.map((s) => (s.initial ? { name: s.name, category: s.category, initial: true } : { name: s.name, category: s.category }));
  out.transitions = doc.transitions.map(cleanTransition);
  const layout: Record<string, Position> = {};
  for (const s of doc.statuses) {
    const p = doc.layout?.[s.name];
    if (p) layout[s.name] = { x: Math.round(p.x), y: Math.round(p.y) };
  }
  if (Object.keys(layout).length) out.layout = layout;
  return out;
}

/** workflowToYaml renders readable block YAML, keeping short leaf maps and lists on one line. */
export function workflowToYaml(doc: WorkflowDoc): string {
  const d = new YamlDocument(toPlain(doc), { version: "1.2" });
  visit(d, {
    Map(_, node) {
      const leaf = node.items.length <= 3 && node.items.every((p) => isScalar(p.value));
      const short = node.items.reduce((n, p) => n + String((p.key as { value?: unknown })?.value ?? "").length + String((p.value as { value?: unknown })?.value ?? "").length + 4, 0) < 64;
      node.flow = leaf && short;
    },
    Seq(_, node) {
      node.flow = node.items.length <= 6 && node.items.every((i) => isScalar(i) || (isMap(i) && i.flow)) && !node.items.some((i) => isSeq(i));
    },
  });
  // Flow sequences of flow maps read badly when long; keep those as blocks.
  visit(d, {
    Seq(_, node) {
      if (node.flow && node.items.some((i) => isMap(i)) && node.items.length > 1) node.flow = false;
    },
  });
  return d.toString({ indent: 2, lineWidth: 0, flowCollectionPadding: false });
}

// ---------------------------------------------------------------------------
// Layout

export const NODE_WIDTH = 168;
export const NODE_HEIGHT = 60;
const GAP_X = 260;
const GAP_Y = 150;

/** withLayout returns a doc whose layout has a position for every status (missing ones placed below). */
export function withLayout(doc: WorkflowDoc): WorkflowDoc {
  const layout: Record<string, Position> = {};
  const known = doc.statuses.filter((s) => doc.layout?.[s.name]);
  for (const s of known) layout[s.name] = doc.layout![s.name];
  const missing = doc.statuses.filter((s) => !doc.layout?.[s.name]);
  if (missing.length === 0 && Object.keys(layout).length === Object.keys(doc.layout ?? {}).length) return doc;
  if (known.length === 0) return { ...doc, layout: tidyLayout(doc) };
  const maxY = Math.max(...known.map((s) => layout[s.name].y));
  const minX = Math.min(...known.map((s) => layout[s.name].x));
  missing.forEach((s, i) => {
    layout[s.name] = { x: minX + (i % 4) * GAP_X, y: maxY + GAP_Y * (1 + Math.floor(i / 4)) };
  });
  return { ...doc, layout };
}

/**
 * tidyLayout arranges statuses in columns by distance from the initial status (breadth-first
 * over transitions); done statuses that are only reachable last end up on the right.
 */
export function tidyLayout(doc: WorkflowDoc): Record<string, Position> {
  const names = doc.statuses.map((s) => s.name);
  const initial = doc.statuses.find((s) => s.initial)?.name ?? names[0];
  const level = new Map<string, number>();
  if (initial !== undefined) level.set(initial, 0);
  const queue = initial !== undefined ? [initial] : [];
  while (queue.length) {
    const cur = queue.shift()!;
    for (const t of doc.transitions) {
      const applies = t.from ? t.from.some((f) => same(f, cur)) : true;
      const target = names.find((n) => same(n, t.to));
      if (!applies || !target || level.has(target)) continue;
      level.set(target, level.get(cur)! + 1);
      queue.push(target);
    }
  }
  let maxLevel = Math.max(0, ...level.values());
  for (const n of names) if (!level.has(n)) level.set(n, ++maxLevel);
  const rows = new Map<number, number>();
  const out: Record<string, Position> = {};
  for (const n of names) {
    const l = level.get(n)!;
    const r = rows.get(l) ?? 0;
    rows.set(l, r + 1);
    out[n] = { x: l * GAP_X, y: 120 + r * GAP_Y };
  }
  // Centre each column vertically around the tallest one.
  const tallest = Math.max(...rows.values(), 1);
  for (const n of names) {
    const count = rows.get(level.get(n)!)!;
    out[n] = { x: out[n].x, y: out[n].y + ((tallest - count) * GAP_Y) / 2 };
  }
  return out;
}

// ---------------------------------------------------------------------------
// Editing operations (pure)

export function findStatus(doc: WorkflowDoc, name: string): StatusDoc | undefined {
  return doc.statuses.find((s) => same(s.name, name));
}

export function uniqueStatusName(doc: WorkflowDoc, base = "New status"): string {
  if (!findStatus(doc, base)) return base;
  for (let n = 2; ; n++) if (!findStatus(doc, `${base} ${n}`)) return `${base} ${n}`;
}

export function uniqueTransitionName(doc: WorkflowDoc, base: string): string {
  const taken = (n: string) => doc.transitions.some((t) => same(t.name, n));
  if (!taken(base)) return base;
  for (let n = 2; ; n++) if (!taken(`${base} (${n})`)) return `${base} (${n})`;
}

export function addStatus(doc: WorkflowDoc, status: Partial<StatusDoc> & { position?: Position } = {}): { doc: WorkflowDoc; name: string } {
  const name = status.name && !findStatus(doc, status.name) ? status.name : uniqueStatusName(doc, status.name || "New status");
  const st: StatusDoc = { name, category: status.category ?? "in_progress" };
  const layout = { ...(doc.layout ?? {}) };
  if (status.position) layout[name] = status.position;
  return { doc: withLayout({ ...doc, statuses: [...doc.statuses, st], layout }), name };
}

/**
 * updateStatus changes a status; renaming updates transitions and layout, and `initial` is
 * exclusive. With `renameAutoNamed`, transitions named after the old status (as new ones are
 * by default) and without a pinned id take the new name too.
 */
export function updateStatus(doc: WorkflowDoc, name: string, patch: Partial<StatusDoc>, opts: { renameAutoNamed?: boolean } = {}): WorkflowDoc {
  const cur = findStatus(doc, name);
  if (!cur) return doc;
  const statuses = doc.statuses.map((s) => {
    const next: StatusDoc = s === cur ? { ...s, ...patch } : patch.initial ? { ...s, initial: false } : { ...s };
    if (!next.initial) delete next.initial;
    return next;
  });
  let transitions = doc.transitions;
  let layout = doc.layout;
  if (patch.name !== undefined && patch.name !== cur.name) {
    const rename = (n: string) => (same(n, cur.name) ? patch.name! : n);
    transitions = transitions.map((t) => ({
      ...t,
      ...(opts.renameAutoNamed && !t.id && same(t.to, cur.name) && same(t.name, cur.name) ? { name: patch.name! } : {}),
      to: rename(t.to),
      ...(t.from ? { from: t.from.map(rename) } : {}),
    }));
    if (layout) {
      layout = Object.fromEntries(Object.entries(layout).map(([k, v]) => [rename(k), v]));
    }
  }
  return { ...doc, statuses, transitions, layout };
}

/** deleteStatus removes a status, transitions into it, and it from other transitions' sources. */
export function deleteStatus(doc: WorkflowDoc, name: string): WorkflowDoc {
  const statuses = doc.statuses.filter((s) => !same(s.name, name));
  const transitions: TransitionDoc[] = [];
  for (const t of doc.transitions) {
    if (same(t.to, name)) continue;
    if (t.from) {
      const from = t.from.filter((f) => !same(f, name));
      if (from.length === 0) continue; // would silently become "from any"
      transitions.push({ ...t, from });
    } else transitions.push(t);
  }
  const layout = doc.layout ? Object.fromEntries(Object.entries(doc.layout).filter(([k]) => !same(k, name))) : undefined;
  return { ...doc, statuses, transitions, layout };
}

export function setPosition(doc: WorkflowDoc, name: string, p: Position): WorkflowDoc {
  return { ...doc, layout: { ...(doc.layout ?? {}), [name]: { x: Math.round(p.x), y: Math.round(p.y) } } };
}

/** addTransition adds `from -> to` (from undefined = from any) and returns its index. */
export function addTransition(doc: WorkflowDoc, from: string | undefined, to: string, name?: string): { doc: WorkflowDoc; index: number } {
  const t: TransitionDoc = { name: uniqueTransitionName(doc, name || to), to, ...(from ? { from: [from] } : {}) };
  return { doc: { ...doc, transitions: [...doc.transitions, t] }, index: doc.transitions.length };
}

export function updateTransition(doc: WorkflowDoc, index: number, patch: Partial<TransitionDoc>): WorkflowDoc {
  return {
    ...doc,
    transitions: doc.transitions.map((t, i) => {
      if (i !== index) return t;
      const next: TransitionDoc = { ...t, ...patch };
      for (const k of Object.keys(next) as (keyof TransitionDoc)[]) {
        const v = next[k];
        if (v === undefined || (k !== "name" && k !== "to" && (v === "" || (Array.isArray(v) && v.length === 0)))) delete next[k];
      }
      return next;
    }),
  };
}

export function deleteTransition(doc: WorkflowDoc, index: number): WorkflowDoc {
  return { ...doc, transitions: doc.transitions.filter((_, i) => i !== index) };
}

/** removeEdge drops one source from a transition; the last source removes the transition. */
export function removeEdge(doc: WorkflowDoc, index: number, from: string): WorkflowDoc {
  const t = doc.transitions[index];
  if (!t?.from) return doc;
  const rest = t.from.filter((f) => !same(f, from));
  return rest.length ? updateTransition(doc, index, { from: rest }) : deleteTransition(doc, index);
}

/** Transitions entering / leaving a status (global transitions count as leaving every status). */
export function transitionsOf(doc: WorkflowDoc, name: string) {
  const incoming: number[] = [];
  const outgoing: number[] = [];
  doc.transitions.forEach((t, i) => {
    if (same(t.to, name)) incoming.push(i);
    if (t.from?.some((f) => same(f, name))) outgoing.push(i);
  });
  return { incoming, outgoing };
}

/** docEqual compares two documents by their serialised form. */
export function docEqual(a: WorkflowDoc, b: WorkflowDoc): boolean {
  return JSON.stringify(toPlain(a)) === JSON.stringify(toPlain(b));
}

// ---------------------------------------------------------------------------
// Validation paths

export type PathTarget = { kind: "status"; index: number } | { kind: "transition"; index: number } | { kind: "workflow" };

/** targetOfPath maps a validation path ("transitions[2].condition") to the element it concerns. */
export function targetOfPath(path: string | undefined): PathTarget {
  const m = /^(statuses|transitions)\[(\d+)\]/.exec(path ?? "");
  if (!m) return { kind: "workflow" };
  return { kind: m[1] === "statuses" ? "status" : "transition", index: Number(m[2]) };
}

/** A starting point for new workflows. */
export function blankWorkflow(name: string): WorkflowDoc {
  return {
    name,
    description: "",
    statuses: [
      { name: "To Do", category: "todo", initial: true },
      { name: "In Progress", category: "in_progress" },
      { name: "Done", category: "done" },
    ],
    transitions: [
      { name: "Start work", from: ["To Do"], to: "In Progress" },
      { name: "Stop work", from: ["In Progress"], to: "To Do" },
      { name: "Done", from: ["In Progress"], to: "Done" },
      { name: "Reopen", from: ["Done"], to: "To Do" },
    ],
    layout: { "To Do": { x: 0, y: 120 }, "In Progress": { x: 260, y: 120 }, Done: { x: 520, y: 120 } },
  };
}

export interface DocDiff {
  addedStatuses: string[];
  removedStatuses: string[];
  changedStatuses: string[];
  addedTransitions: string[];
  removedTransitions: string[];
  changedTransitions: string[];
  layoutChanged: boolean;
  descriptionChanged: boolean;
}

/** diffDocs summarises what a draft changes compared with the published version. */
export function diffDocs(before: WorkflowDoc, after: WorkflowDoc): DocDiff {
  const key = (s: string) => s.trim().toLowerCase();
  const sb = new Map(before.statuses.map((s) => [key(s.name), s]));
  const sa = new Map(after.statuses.map((s) => [key(s.name), s]));
  const tb = new Map(before.transitions.map((t) => [key(t.name), JSON.stringify(cleanTransition(t))]));
  const ta = new Map(after.transitions.map((t) => [key(t.name), JSON.stringify(cleanTransition(t))]));
  const pb = toPlain(before);
  const pa = toPlain(after);
  return {
    addedStatuses: after.statuses.filter((s) => !sb.has(key(s.name))).map((s) => s.name),
    removedStatuses: before.statuses.filter((s) => !sa.has(key(s.name))).map((s) => s.name),
    changedStatuses: after.statuses
      .filter((s) => {
        const o = sb.get(key(s.name));
        return o && (o.category !== s.category || !!o.initial !== !!s.initial);
      })
      .map((s) => s.name),
    addedTransitions: after.transitions.filter((t) => !tb.has(key(t.name))).map((t) => t.name),
    removedTransitions: before.transitions.filter((t) => !ta.has(key(t.name))).map((t) => t.name),
    changedTransitions: after.transitions.filter((t) => tb.has(key(t.name)) && tb.get(key(t.name)) !== ta.get(key(t.name))).map((t) => t.name),
    layoutChanged: JSON.stringify(pb.layout ?? {}) !== JSON.stringify(pa.layout ?? {}),
    descriptionChanged: (before.description ?? "") !== (after.description ?? ""),
  };
}
