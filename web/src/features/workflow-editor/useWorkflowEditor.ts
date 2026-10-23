import { useCallback, useMemo, useReducer } from "react";
import { docEqual, parseWorkflowYaml, withLayout, workflowToYaml, type WorkflowDoc } from "./model";

export type Selection = { kind: "status"; name: string } | { kind: "transition"; index: number; from?: string } | null;

/** One undoable state: the document, plus the YAML text when it was typed by hand. */
interface Snapshot {
  doc: WorkflowDoc;
  /** Hand-edited YAML (kept verbatim, comments included); null = generated from doc. */
  text: string | null;
  /** Statuses of the published version renamed in this draft: old name -> current name. */
  renames: Record<string, string>;
}

interface State {
  present: Snapshot;
  past: Snapshot[];
  future: Snapshot[];
  /** Coalesces rapid edits of one field into a single undo step. */
  lastKey?: string;
  lastAt?: number;
  baseline: WorkflowDoc;
  baselineText: string;
  selection: Selection;
  /** Parse error of hand-edited YAML; the canvas keeps the last valid document. */
  parseError?: { message: string; line?: number };
}

type Action =
  | { type: "apply"; fn: (d: WorkflowDoc) => WorkflowDoc; key?: string; select?: Selection; rename?: { from: string; to: string } }
  | { type: "text"; text: string }
  | { type: "select"; selection: Selection }
  | { type: "undo" }
  | { type: "redo" }
  | { type: "reset"; text: string; doc: WorkflowDoc }
  | { type: "load"; text: string; doc: WorkflowDoc };

const HISTORY = 100;
const COALESCE_MS = 1200;

function trackRename(renames: Record<string, string>, baseline: WorkflowDoc, from: string, to: string) {
  const out: Record<string, string> = {};
  let found = false;
  for (const [k, v] of Object.entries(renames)) {
    if (v === from) {
      found = true;
      if (k !== to) out[k] = to;
    } else out[k] = v;
  }
  if (!found && baseline.statuses.some((s) => s.name === from) && from !== to) out[from] = to;
  return out;
}

function push(state: State, next: Snapshot, key?: string): State {
  const now = Date.now();
  const coalesce = key !== undefined && key === state.lastKey && state.lastAt !== undefined && now - state.lastAt < COALESCE_MS;
  return {
    ...state,
    present: next,
    past: coalesce ? state.past : [...state.past, state.present].slice(-HISTORY),
    future: [],
    lastKey: key,
    lastAt: now,
  };
}

function reducer(state: State, a: Action): State {
  switch (a.type) {
    case "apply": {
      const doc = a.fn(state.present.doc);
      const renames = a.rename ? trackRename(state.present.renames, state.baseline, a.rename.from, a.rename.to) : state.present.renames;
      const selection = a.select === undefined ? state.selection : a.select;
      if (doc === state.present.doc || (state.present.text === null && docEqual(doc, state.present.doc) && !a.rename)) {
        return { ...state, selection };
      }
      return { ...push(state, { doc, text: null, renames }, a.key), selection, parseError: undefined };
    }
    case "text": {
      const r = parseWorkflowYaml(a.text);
      if (!r.ok) {
        return { ...push(state, { ...state.present, text: a.text }, "yaml"), parseError: { message: r.error, line: r.line } };
      }
      return { ...push(state, { doc: withLayout(r.doc), text: a.text, renames: state.present.renames }, "yaml"), parseError: undefined };
    }
    case "select":
      return { ...state, selection: a.selection };
    case "undo": {
      const prev = state.past[state.past.length - 1];
      if (!prev) return state;
      return { ...state, present: prev, past: state.past.slice(0, -1), future: [state.present, ...state.future], lastKey: undefined, selection: null, parseError: undefined };
    }
    case "redo": {
      const next = state.future[0];
      if (!next) return state;
      return { ...state, present: next, past: [...state.past, state.present], future: state.future.slice(1), lastKey: undefined, selection: null, parseError: undefined };
    }
    case "reset":
      return init({ text: a.text, doc: a.doc });
    case "load":
      return { ...push(state, { doc: withLayout(a.doc), text: a.text, renames: {} }), selection: null, parseError: undefined };
  }
}

function init({ text, doc }: { text: string; doc: WorkflowDoc }): State {
  const laid = withLayout(doc);
  return { present: { doc: laid, text, renames: {} }, past: [], future: [], baseline: laid, baselineText: text, selection: null };
}

/**
 * useWorkflowEditor holds the draft of a workflow: the document edited on the canvas and in
 * panels, or YAML typed by hand, with undo/redo and the current selection.
 */
export function useWorkflowEditor(initial: { text: string; doc: WorkflowDoc }) {
  const [state, dispatch] = useReducer(reducer, initial, init);
  const { present } = state;
  const yaml = useMemo(() => present.text ?? workflowToYaml(present.doc), [present]);
  const dirty = present.text !== null ? present.text !== state.baselineText : !docEqual(present.doc, state.baseline);

  const apply = useCallback(
    (fn: (d: WorkflowDoc) => WorkflowDoc, opts: { key?: string; select?: Selection; rename?: { from: string; to: string } } = {}) => dispatch({ type: "apply", fn, ...opts }),
    [],
  );
  const select = useCallback((selection: Selection) => dispatch({ type: "select", selection }), []);
  const setText = useCallback((text: string) => dispatch({ type: "text", text }), []);
  const undo = useCallback(() => dispatch({ type: "undo" }), []);
  const redo = useCallback(() => dispatch({ type: "redo" }), []);
  const reset = useCallback((text: string, doc: WorkflowDoc) => dispatch({ type: "reset", text, doc }), []);
  const load = useCallback((text: string, doc: WorkflowDoc) => dispatch({ type: "load", text, doc }), []);

  return {
    doc: present.doc,
    yaml,
    handEdited: present.text !== null && present.text !== state.baselineText,
    renames: present.renames,
    baseline: state.baseline,
    selection: state.selection,
    parseError: state.parseError,
    dirty,
    canUndo: state.past.length > 0,
    canRedo: state.future.length > 0,
    apply,
    select,
    setText,
    undo,
    redo,
    reset,
    load,
  };
}

export type WorkflowEditor = ReturnType<typeof useWorkflowEditor>;
