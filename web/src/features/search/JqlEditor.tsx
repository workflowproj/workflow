import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { CircleAlert, CornerDownLeft } from "lucide-react";
import { cn } from "@/lib/cn";
import { useEscapeLayer } from "@/lib/escape";
import { analyze, applyCompletion, classify, tokenize, type Highlight } from "./jql";
import { useJqlSuggestions, type Suggestion } from "./use-jql-suggestions";

export type JqlError = { message: string; index?: number };

const tone: Record<Highlight, string> = {
  field: "text-accent",
  keyword: "font-semibold text-ink",
  operator: "text-muted",
  string: "text-success",
  function: "text-warning",
  value: "text-ink",
  punct: "text-faint",
  ws: "",
};

const kindTone: Record<Suggestion["kind"], string> = {
  field: "text-accent",
  operator: "text-muted",
  value: "text-ink",
  keyword: "font-semibold text-ink",
  function: "text-warning",
};

// The textarea and the highlight layer must lay text out identically.
const textBox = "px-3 py-[7px] font-mono text-[13px] leading-[22px] whitespace-pre-wrap [overflow-wrap:anywhere]";

/**
 * JqlEditor is a query input with syntax highlighting, autocomplete of fields, operators and
 * values (from GET /search/fields and friends) and inline syntax errors. Enter runs the query;
 * Tab or Enter on a highlighted suggestion completes it; Ctrl+Space opens suggestions.
 */
export function JqlEditor({
  value,
  onChange,
  onRun,
  error,
  projectKey,
  placeholder = 'project = PLAT AND status = "In Progress" ORDER BY updated DESC',
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  onRun: (v: string) => void;
  error?: JqlError | null;
  projectKey?: string;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const anchor = useRef<HTMLSpanElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const pendingCaret = useRef<number | null>(null);
  const listId = useId();
  const errorId = useId();
  const [caret, setCaret] = useState<number | null>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [pos, setPos] = useState({ left: 0, top: 30 });

  const ctx = useMemo(() => (caret === null ? null : analyze(value, caret)), [value, caret]);
  const { items, hint } = useJqlSuggestions(open ? ctx : null, projectKey);
  const showList = open && ctx !== null && (items.length > 0 || !!hint);
  useEscapeLayer(showList, () => setOpen(false));

  // Highlight the first suggestion only once something has been typed, so Enter still runs.
  useEffect(() => setActive(ctx?.prefix ? 0 : -1), [ctx?.from, ctx?.prefix, ctx?.expect]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
    if (pendingCaret.current !== null) {
      el.setSelectionRange(pendingCaret.current, pendingCaret.current);
      setCaret(pendingCaret.current);
      pendingCaret.current = null;
    }
  }, [value]);

  useLayoutEffect(() => {
    if (!showList || !anchor.current || !box.current) return;
    const width = box.current.clientWidth;
    setPos({ left: Math.max(0, Math.min(anchor.current.offsetLeft - 8, width - 320)), top: anchor.current.offsetTop + 26 });
  }, [showList, ctx?.from, value]);

  const sync = () => {
    const el = ref.current;
    if (el) setCaret(el.selectionStart === el.selectionEnd ? el.selectionStart : null);
  };

  const accept = (s: Suggestion) => {
    if (!ctx) return;
    const r = applyCompletion(value, ctx, s.insert, s.tight);
    pendingCaret.current = r.cursor;
    onChange(r.text);
    setOpen(true);
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === " " && e.ctrlKey) {
      e.preventDefault();
      sync();
      setOpen(true);
      return;
    }
    if (showList && items.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((a) => (a + 1) % items.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActive((a) => (a <= 0 ? items.length - 1 : a - 1));
        return;
      }
      if (e.key === "Tab" && !e.shiftKey) {
        e.preventDefault();
        accept(items[Math.max(active, 0)]);
        return;
      }
      if (e.key === "Enter" && active >= 0) {
        e.preventDefault();
        accept(items[active]);
        return;
      }
    }
    if (e.key === "Enter") {
      e.preventDefault();
      setOpen(false);
      onRun(value);
    }
  };

  return (
    <div className="relative">
      <div
        ref={box}
        className={cn(
          "relative rounded-md border bg-surface transition-colors focus-within:ring-2",
          error ? "border-danger focus-within:ring-danger/20" : "border-line-strong focus-within:border-accent focus-within:ring-accent/25",
        )}
      >
        <pre aria-hidden className={cn(textBox, "pointer-events-none absolute inset-0 m-0 overflow-hidden text-ink")}>
          <Highlighted text={value} errorAt={error?.index} anchorAt={ctx?.from} anchorRef={anchor} />
        </pre>
        <textarea
          ref={ref}
          rows={1}
          autoFocus={autoFocus}
          spellCheck={false}
          autoCapitalize="off"
          autoComplete="off"
          value={value}
          placeholder={placeholder}
          role="combobox"
          aria-label="Search query"
          aria-expanded={showList}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={showList && active >= 0 ? `${listId}-${active}` : undefined}
          aria-invalid={!!error}
          aria-describedby={error ? errorId : undefined}
          onChange={(e) => {
            onChange(e.target.value.replace(/\n/g, " "));
            setCaret(e.target.selectionStart);
            setOpen(true);
          }}
          onKeyDown={onKeyDown}
          onKeyUp={(e) => ["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key) && sync()}
          onClick={() => {
            sync();
            setOpen(true);
          }}
          onFocus={() => {
            sync();
            setOpen(true);
          }}
          onBlur={() => {
            setOpen(false);
            setCaret(null);
          }}
          className={cn(
            textBox,
            "relative block min-h-9 w-full resize-none overflow-hidden bg-transparent text-transparent caret-ink outline-none placeholder:text-faint selection:bg-accent/25",
          )}
        />
        {showList && (
          <ul
            id={listId}
            role="listbox"
            style={{ left: pos.left, top: pos.top }}
            className="absolute z-30 max-h-80 w-80 overflow-y-auto rounded-md border border-line bg-surface p-1 shadow-lg"
            onMouseDown={(e) => e.preventDefault()}
          >
            {items.map((s, i) => (
              <li
                key={`${s.kind}:${s.label}:${s.insert}`}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseEnter={() => setActive(i)}
                onClick={() => accept(s)}
                className={cn("flex cursor-pointer items-center gap-3 rounded px-2.5 py-1.5 text-[13px]", i === active && "bg-surface-2")}
              >
                <span className={cn("min-w-0 truncate font-mono", kindTone[s.kind])}>{s.label}</span>
                {s.detail && <span className="ml-auto shrink-0 truncate text-xs text-faint">{s.detail}</span>}
                {i === active && !s.detail && <CornerDownLeft className="ml-auto size-3 shrink-0 text-faint" />}
              </li>
            ))}
            {items.length === 0 && hint && <li className="px-2.5 py-1.5 text-xs text-muted">{hint}</li>}
          </ul>
        )}
      </div>
      {error && (
        <p id={errorId} role="alert" className="mt-1.5 flex items-start gap-1.5 text-[13px] text-danger">
          <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
          <span>
            {error.message}
            {error.index !== undefined && <span className="text-danger/70"> (at character {error.index + 1})</span>}
          </span>
        </p>
      )}
    </div>
  );
}

/** Highlighted renders the query as coloured tokens, marking the error and the completion anchor. */
function Highlighted({ text, errorAt, anchorAt, anchorRef }: { text: string; errorAt?: number; anchorAt?: number; anchorRef: React.RefObject<HTMLSpanElement | null> }) {
  const tokens = tokenize(text);
  const classes = classify(tokens);
  const out: ReactNode[] = [];
  const mark = <span key="anchor" ref={anchorRef} />;
  let anchored = false;
  tokens.forEach((t, i) => {
    const err = errorAt !== undefined && errorAt >= t.start && errorAt < t.end && t.kind !== "ws";
    const cls = cn(tone[classes[i]], err && "rounded-sm bg-danger-soft underline decoration-danger decoration-wavy underline-offset-4");
    if (anchorAt !== undefined && !anchored && anchorAt >= t.start && anchorAt < t.end) {
      anchored = true;
      out.push(
        <span key={i} className={cls}>
          {t.text.slice(0, anchorAt - t.start)}
          {mark}
          {t.text.slice(anchorAt - t.start)}
        </span>,
      );
    } else {
      out.push(
        <span key={i} className={cls}>
          {t.text}
        </span>,
      );
    }
  });
  // An error at the very end ("expected a value") gets a visible marker.
  const errorAtEnd = errorAt !== undefined && errorAt >= text.length;
  if (!anchored) out.push(mark);
  if (errorAtEnd && text.length > 0) out.push(<span key="eof" className="ml-px inline-block h-[1.1em] w-2 translate-y-[3px] rounded-sm border-b-2 border-danger bg-danger-soft" />);
  return <>{out}</>;
}
