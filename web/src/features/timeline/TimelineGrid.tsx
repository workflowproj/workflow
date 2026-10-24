import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import type { Issue } from "@/api/types";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Bar, BAR_H, BAR_TOP, LinkHandle, ROW_H, type DragMode } from "./Bar";
import { Dependencies } from "./Dependencies";
import { MarkerLane, ReleaseLines, SprintBands } from "./Markers";
import { formatSpan, fromDay, isOverdue, toDay, weekday, type Dependency, type Row, type Schedule, type TimelineData, type TimelineLink } from "./model";
import { DatePopover, LinkMenu } from "./Popovers";
import { gridLines, ppdFor, snapDays, timeRange, type Zoom } from "./scale";
import { BAND_H, LANE_H, TimeHeader } from "./TimeHeader";
import { TREE_W, TreeCell } from "./TreeCell";

const OVERSCAN = 8;

type Drag =
  | { kind: "bar"; mode: DragMode; issue: Issue; base: Schedule; clientX0: number; scroll0: number; delta: number; moved: boolean; editable: boolean }
  | { kind: "create"; issue: Issue; anchor: number; current: number }
  | { kind: "link"; from: string; x0: number; y0: number; x: number; y: number; target?: string };

/** preview applies a bar drag to its schedule: move shifts both ends, resizing keeps start ≤ end. */
function preview(d: Extract<Drag, { kind: "bar" }>): { start: number; end: number } {
  const { start, end } = d.base;
  if (d.mode === "move") return { start: start + d.delta, end: end + d.delta };
  if (d.mode === "start") return { start: Math.min(start + d.delta, end), end };
  return { start, end: Math.max(end + d.delta, start) };
}

export type TimelineEdits = {
  schedule: (issue: Issue, start: string | null, due: string | null) => void;
  link: (source: string, target: string) => void;
  unlink: (l: TimelineLink) => void;
};

/**
 * TimelineGrid is the scrolling tree + time grid. Rows are windowed, so a few hundred issues
 * stay smooth. Bars can be dragged (move, resize, draw on an empty row), dependencies drawn
 * from a bar's end handle onto another bar, and every bar has a date popover when focused.
 */
export function TimelineGrid({
  data,
  rows,
  sched,
  deps,
  warnings,
  progress,
  zoom,
  multi,
  collapsed,
  onToggle,
  onToggleAll,
  canEdit,
  onOpen,
  edits,
  todaySignal,
  today,
}: {
  data: TimelineData;
  rows: Row[];
  sched: Map<string, Schedule>;
  deps: Dependency[];
  warnings: Map<string, string[]>;
  progress?: Map<string, { done: number; total: number }>;
  zoom: Zoom;
  multi: boolean;
  collapsed: Set<string>;
  onToggle: (id: string) => void;
  onToggleAll: (collapse: boolean) => void;
  canEdit: (projectKey: string) => boolean;
  onOpen: (key: string) => void;
  edits: TimelineEdits;
  todaySignal: number;
  today: number;
}) {
  const ppd = ppdFor(zoom);
  const range = useMemo(() => {
    const days: number[] = [];
    for (const s of sched.values()) days.push(s.start, s.end);
    for (const { sprint } of data.sprints) for (const d of [sprint.startDate, sprint.endDate]) if (d) days.push(toDay(d));
    for (const { release } of data.releases) if (release.releaseDate) days.push(toDay(release.releaseDate));
    return timeRange(days, today);
  }, [sched, data.sprints, data.releases, today]);
  const gridW = (range.end - range.start + 1) * ppd;
  const x = useCallback((day: number) => (day - range.start) * ppd, [range.start, ppd]);
  const headerH = BAND_H * 2 + (multi ? 0 : LANE_H);
  const bodyH = rows.length * ROW_H;

  const rowOf = useMemo(() => {
    const m = new Map<string, number>();
    rows.forEach((r, i) => r.kind === "issue" && m.set(r.id, i));
    return m;
  }, [rows]);
  const spans = useMemo(() => {
    const m = new Map<string, [number, number]>();
    rows.forEach((r, i) => {
      const key = r.kind === "project" ? r.project.key : r.projectKey;
      const s = m.get(key);
      m.set(key, s ? [s[0], i] : [i, i]);
    });
    return m;
  }, [rows]);
  const conflictKeys = useMemo(() => new Set(warnings.keys()), [warnings]);

  // Scrolling and windowing.
  const scroller = useRef<HTMLDivElement>(null);
  const [win, setWin] = useState({ first: 0, last: 40 });
  const measure = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const first = Math.max(0, Math.floor(el.scrollTop / ROW_H) - OVERSCAN);
    const last = Math.ceil((el.scrollTop + el.clientHeight - headerH) / ROW_H) + OVERSCAN;
    setWin((w) => (w.first === first && w.last === last ? w : { first, last }));
  }, [headerH]);
  useEffect(() => {
    measure();
    const el = scroller.current;
    if (!el) return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);
  const frame = useRef(0);
  const onScroll = () => {
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(measure);
  };

  // Keep the date at the centre in place when the zoom or the range start changes.
  const prev = useRef({ ppd, start: range.start, ready: false });
  useLayoutEffect(() => {
    const el = scroller.current;
    const p = prev.current;
    if (el && p.ready && (p.ppd !== ppd || p.start !== range.start)) {
      const viewW = el.clientWidth - TREE_W;
      const center = p.start + (el.scrollLeft + viewW / 2) / p.ppd;
      el.scrollLeft = (center - range.start) * ppd - viewW / 2;
    }
    prev.current = { ppd, start: range.start, ready: true };
  }, [ppd, range.start]);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el) el.scrollLeft = x(today) - (el.clientWidth - TREE_W) * 0.3;
    // Only on mount and when asked ("Today").
  }, [todaySignal]);

  // Interaction state.
  const [drag, setDragState] = useState<Drag | null>(null);
  const dragRef = useRef<Drag | null>(null);
  const setDrag = (d: Drag | null) => {
    dragRef.current = d;
    setDragState(d);
  };
  const [selected, setSelected] = useState<string | null>(null);
  const [hover, setHover] = useState<string | null>(null);
  const [hint, setHint] = useState<{ key: string; day: number } | null>(null);
  const [linkMenu, setLinkMenu] = useState<{ link: TimelineLink; left: number; top: number } | null>(null);

  /** Converts a pointer position to grid coordinates (x from the grid's start, y from the first row). */
  const point = (clientX: number, clientY: number) => {
    const el = scroller.current!;
    const box = el.getBoundingClientRect();
    return { x: clientX - box.left + el.scrollLeft - TREE_W, y: clientY - box.top + el.scrollTop - headerH };
  };
  const dayAt = (gx: number) => range.start + Math.floor(gx / ppd);
  const autoScroll = (e: PointerEvent, vertical: boolean) => {
    const el = scroller.current!;
    const box = el.getBoundingClientRect();
    if (e.clientX > box.right - 40) el.scrollLeft += 14;
    else if (e.clientX < box.left + TREE_W + 40) el.scrollLeft -= 14;
    if (vertical) {
      if (e.clientY > box.bottom - 32) el.scrollTop += 14;
      else if (e.clientY < box.top + headerH + 32) el.scrollTop -= 14;
    }
  };

  const dragging = drag !== null;
  useEffect(() => {
    if (!dragging) return;
    const onMove = (e: PointerEvent) => {
      const d = dragRef.current;
      if (!d) return;
      if (d.kind === "bar") {
        const el = scroller.current!;
        const moved = d.moved || Math.abs(e.clientX - d.clientX0) > 3;
        if (!d.editable) return moved !== d.moved && setDrag({ ...d, moved });
        autoScroll(e, false);
        setDrag({ ...d, moved, delta: snapDays(e.clientX - d.clientX0 + el.scrollLeft - d.scroll0, ppd) });
      } else if (d.kind === "create") {
        autoScroll(e, false);
        setDrag({ ...d, current: dayAt(point(e.clientX, e.clientY).x) });
      } else {
        autoScroll(e, true);
        const p = point(e.clientX, e.clientY);
        const hit = (document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null)?.closest<HTMLElement>("[data-bar-key]");
        const target = hit?.dataset.barKey;
        setDrag({ ...d, x: p.x, y: p.y, target: target && target !== d.from ? target : undefined });
      }
    };
    const onUp = () => {
      const d = dragRef.current;
      setDrag(null);
      if (!d) return;
      if (d.kind === "bar") {
        if (!d.moved) return onOpen(d.issue.key);
        if (!d.editable || d.delta === 0) return;
        const s = preview(d);
        edits.schedule(d.issue, fromDay(s.start), fromDay(s.end));
      } else if (d.kind === "create") {
        if (d.current === d.anchor) return;
        edits.schedule(d.issue, fromDay(Math.min(d.anchor, d.current)), fromDay(Math.max(d.anchor, d.current)));
      } else if (d.target) edits.link(d.from, d.target);
    };
    const onKey = (e: globalThis.KeyboardEvent) => e.key === "Escape" && setDrag(null);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", () => setDrag(null));
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("keydown", onKey);
    };
    // The handlers read the live drag from the ref; they only need binding once per drag.
  }, [dragging]);

  const startBar = (e: ReactPointerEvent, issue: Issue, mode: DragMode) => {
    const base = sched.get(issue.key);
    if (e.button !== 0 || !base) return;
    e.preventDefault(); // no text selection, and no focus (focus is the keyboard path)
    setLinkMenu(null);
    setDrag({ kind: "bar", mode, issue, base, clientX0: e.clientX, scroll0: scroller.current!.scrollLeft, delta: 0, moved: false, editable: canEdit(issue.projectKey) });
  };
  const startLink = (e: ReactPointerEvent, issue: Issue, rowIndex: number) => {
    const s = sched.get(issue.key);
    if (e.button !== 0 || !s) return;
    e.preventDefault();
    e.stopPropagation();
    const x0 = x(s.end + 1);
    const y0 = rowIndex * ROW_H + ROW_H / 2;
    setDrag({ kind: "link", from: issue.key, x0, y0, x: x0, y: y0 });
  };

  // Up and down move between bars; Enter opens; Escape lets go.
  const focusBar = (key: string) => {
    const i = rowOf.get(key);
    const el = scroller.current;
    if (i == null || !el) return;
    if (i * ROW_H < el.scrollTop || (i + 1) * ROW_H > el.scrollTop + el.clientHeight - headerH) el.scrollTop = i * ROW_H - (el.clientHeight - headerH) / 2;
    requestAnimationFrame(() => requestAnimationFrame(() => el.querySelector<HTMLElement>(`[data-bar-key="${key}"]`)?.focus()));
  };
  const onBarKey = (e: KeyboardEvent, issue: Issue) => {
    if (e.key === "Enter") {
      e.preventDefault();
      onOpen(issue.key);
    } else if (e.key === "Escape") (e.currentTarget as HTMLElement).blur();
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const start = rowOf.get(issue.key) ?? 0;
      const step = e.key === "ArrowDown" ? 1 : -1;
      for (let i = start + step; i >= 0 && i < rows.length; i += step) {
        const r = rows[i];
        if (r.kind === "issue" && sched.has(r.id)) return focusBar(r.id);
      }
    }
  };

  const lines = useMemo(() => gridLines(range, zoom), [range, zoom]);
  const weekendOffset = ((6 - weekday(range.start) + 7) % 7) * ppd;
  const sprintsOf = (key: string) => data.sprints.filter((s) => s.projectKey === key).map((s) => s.sprint);
  const releasesOf = (key: string) => data.releases.filter((r) => r.projectKey === key).map((r) => r.release);
  const summaries = useMemo(() => new Map(data.issues.map((i) => [i.key, i.summary])), [data.issues]);
  const visible = rows.slice(win.first, win.last + 1);
  const hovered = hover && !drag && hover !== selected ? data.issues.find((i) => i.key === hover) : undefined;

  return (
    <div ref={scroller} onScroll={onScroll} className={cn("relative min-h-0 flex-1 overflow-auto overscroll-contain", drag?.kind === "link" && "cursor-crosshair")}>
      <div className="relative" style={{ width: TREE_W + gridW, height: headerH + bodyH }}>
        <div className="sticky top-0 z-30 flex" style={{ width: TREE_W + gridW, height: headerH }}>
          <div className="sticky left-0 z-10 flex shrink-0 items-end justify-between border-r border-b border-line bg-surface px-3 pb-1.5" style={{ width: TREE_W }}>
            <span className="text-xs font-semibold tracking-wide text-faint uppercase">Work</span>
            <span className="flex">
              <IconButton label="Expand all" size="sm" className="size-6" onClick={() => onToggleAll(false)}>
                <ChevronsUpDown className="size-3.5" />
              </IconButton>
              <IconButton label="Collapse all" size="sm" className="size-6" onClick={() => onToggleAll(true)}>
                <ChevronsDownUp className="size-3.5" />
              </IconButton>
            </span>
          </div>
          <TimeHeader
            range={range}
            zoom={zoom}
            x={x}
            ppd={ppd}
            width={gridW}
            today={today}
            lane={!multi && data.projects[0] ? <MarkerLane sprints={sprintsOf(data.projects[0].key)} releases={releasesOf(data.projects[0].key)} x={x} ppd={ppd} today={today} height={LANE_H} /> : undefined}
          />
        </div>

        <div className="absolute left-0" style={{ top: headerH, width: TREE_W + gridW, height: bodyH }}>
          {/* Background: weekends, grid lines, sprints, releases, today; then the arrows. */}
          <div className="absolute top-0" style={{ left: TREE_W, width: gridW, height: bodyH }}>
            {zoom === "weeks" && (
              <div
                aria-hidden
                className="absolute inset-0"
                style={{
                  backgroundImage: `repeating-linear-gradient(to right, color-mix(in srgb, var(--color-surface-3) 45%, transparent) 0 ${2 * ppd}px, transparent ${2 * ppd}px ${7 * ppd}px)`,
                  backgroundPosition: `${weekendOffset}px 0`,
                }}
              />
            )}
            {lines.map((l) => (
              <div key={l.day} aria-hidden className={cn("absolute top-0 w-px", l.strong ? "bg-line-strong/70" : "bg-line/70")} style={{ left: x(l.day), height: bodyH }} />
            ))}
            {[...spans].map(([key, [a, b]]) => (
              <div key={key}>
                <SprintBands sprints={sprintsOf(key)} x={x} ppd={ppd} top={a * ROW_H} height={(b - a + 1) * ROW_H} />
                <ReleaseLines releases={releasesOf(key)} x={x} ppd={ppd} top={a * ROW_H} height={(b - a + 1) * ROW_H} />
              </div>
            ))}
            {today >= range.start && today <= range.end && <div aria-hidden className="absolute top-0 z-[1] w-0.5 bg-accent" style={{ left: x(today) + ppd / 2 - 1, height: bodyH }} />}
            <Dependencies deps={deps} rowOf={rowOf} x={x} width={gridW} height={bodyH} selected={linkMenu?.link.id} onSelect={(link, at) => {
              const p = point(at.clientX, at.clientY);
              setLinkMenu({ link, left: TREE_W + Math.min(p.x + 8, gridW - 290), top: p.y + 8 });
            }} />
          </div>
          <div aria-hidden className="sticky left-0 z-10 h-full border-r border-line bg-surface" style={{ width: TREE_W }} />

          {visible.map((row, k) => {
            const i = win.first + k;
            return (
              <div key={row.id} data-row-key={row.id} className="group/row absolute left-0 flex" style={{ top: i * ROW_H, height: ROW_H, width: TREE_W + gridW }}>
                <div
                  className={cn(
                    "sticky left-0 z-10 shrink-0 border-r border-b border-line bg-surface group-hover/row:bg-surface-2",
                    row.kind === "project" && "bg-surface-2",
                    row.kind === "issue" && selected === row.id && "bg-accent-soft/60",
                  )}
                  style={{ width: TREE_W }}
                >
                  <TreeCell
                    row={row}
                    collapsed={collapsed.has(row.id)}
                    onToggle={() => onToggle(row.id)}
                    onOpen={onOpen}
                    warnings={row.kind === "issue" ? warnings.get(row.id) : undefined}
                    overdue={row.kind === "issue" && isOverdue(row.issue, today)}
                  />
                </div>
                <div className={cn("relative border-b border-line/50 group-hover/row:bg-surface-2/40", row.kind === "project" && "bg-surface-2/70")} style={{ width: gridW }}>
                  {row.kind === "project" && (
                    <MarkerLane sprints={sprintsOf(row.project.key)} releases={releasesOf(row.project.key)} x={x} ppd={ppd} today={today} height={ROW_H} />
                  )}
                  {row.kind === "issue" && (
                    <IssueLane
                      issue={row.issue}
                      dayAtClient={(clientX) => dayAt(point(clientX, 0).x)}
                      schedule={sched.get(row.id)}
                      x={x}
                      ppd={ppd}
                      editable={canEdit(row.projectKey)}
                      progress={progress?.get(row.id)}
                      conflict={conflictKeys.has(row.id)}
                      drag={drag}
                      selected={selected === row.id}
                      hint={hint?.key === row.id ? hint.day : undefined}
                      linkTarget={drag?.kind === "link" && drag.target === row.id}
                      onBarPointerDown={(e, mode) => startBar(e, row.issue, mode)}
                      onLinkPointerDown={(e) => startLink(e, row.issue, i)}
                      onCreatePointerDown={(e) => {
                        if (e.button !== 0) return;
                        e.preventDefault();
                        const day = dayAt(point(e.clientX, e.clientY).x);
                        setDrag({ kind: "create", issue: row.issue, anchor: day, current: day });
                      }}
                      onHint={(day) => setHint(day == null ? null : { key: row.id, day })}
                      onBarKey={(e) => onBarKey(e, row.issue)}
                      onSelect={(on) => setSelected((s) => (on ? row.id : s === row.id ? null : s))}
                      onHover={(on) => setHover((h) => (on ? row.id : h === row.id ? null : h))}
                      onSave={(start, due) => edits.schedule(row.issue, start, due)}
                      onOpen={() => onOpen(row.id)}
                    />
                  )}
                </div>
              </div>
            );
          })}

          {drag?.kind === "link" && (
            <svg aria-hidden className="pointer-events-none absolute top-0 z-[7] overflow-visible" style={{ left: TREE_W }} width={gridW} height={bodyH}>
              <path d={`M${drag.x0},${drag.y0}C${drag.x0 + 40},${drag.y0} ${drag.x - 40},${drag.y} ${drag.x},${drag.y}`} fill="none" stroke="var(--color-accent)" strokeWidth={2} strokeDasharray="5 4" />
              <circle cx={drag.x} cy={drag.y} r={4} fill="var(--color-accent)" />
            </svg>
          )}
          {hovered && <HoverCard issue={hovered} schedule={sched.get(hovered.key)} warnings={warnings.get(hovered.key)} progress={progress?.get(hovered.key)} left={TREE_W + Math.max(8, x(sched.get(hovered.key)?.start ?? range.start))} top={((rowOf.get(hovered.key) ?? 0) + 1) * ROW_H - 4} />}
          {linkMenu && (
            <LinkMenu
              link={linkMenu.link}
              conflict={!!deps.find((d) => d.link.id === linkMenu.link.id)?.conflict}
              left={linkMenu.left}
              top={linkMenu.top}
              summaries={summaries}
              canRemove={!linkMenu.link.id.startsWith("pending-") && canEdit(linkMenu.link.sourceKey.split("-")[0])}
              onRemove={() => {
                edits.unlink(linkMenu.link);
                setLinkMenu(null);
              }}
              onOpen={(key) => {
                setLinkMenu(null);
                onOpen(key);
              }}
              onClose={() => setLinkMenu(null)}
            />
          )}
        </div>
      </div>
    </div>
  );
}

/** IssueLane is the grid part of an issue row: its bar (with drag preview), handles, hint and popover. */
function IssueLane({
  issue,
  dayAtClient,
  schedule,
  x,
  ppd,
  editable,
  progress,
  conflict,
  drag,
  selected,
  hint,
  linkTarget,
  onBarPointerDown,
  onLinkPointerDown,
  onCreatePointerDown,
  onHint,
  onBarKey,
  onSelect,
  onHover,
  onSave,
  onOpen,
}: {
  issue: Issue;
  dayAtClient: (clientX: number) => number;
  schedule?: Schedule;
  x: (day: number) => number;
  ppd: number;
  editable: boolean;
  progress?: { done: number; total: number };
  conflict: boolean;
  drag: Drag | null;
  selected: boolean;
  hint?: number;
  linkTarget: boolean;
  onBarPointerDown: (e: ReactPointerEvent, mode: DragMode) => void;
  onLinkPointerDown: (e: ReactPointerEvent) => void;
  onCreatePointerDown: (e: ReactPointerEvent) => void;
  onHint: (day: number | null) => void;
  onBarKey: (e: KeyboardEvent) => void;
  onSelect: (on: boolean) => void;
  onHover: (on: boolean) => void;
  onSave: (start: string | null, due: string | null) => void;
  onOpen: () => void;
}) {
  const barDrag = drag?.kind === "bar" && drag.issue.key === issue.key && drag.editable && drag.moved ? drag : null;
  const createDrag = drag?.kind === "create" && drag.issue.key === issue.key ? drag : null;
  const shown = schedule && barDrag ? { ...schedule, ...preview(barDrag) } : schedule;
  const chip = (start: number, end: number) => (
    <span className="pointer-events-none absolute z-30 rounded bg-ink px-1.5 py-0.5 text-[10px] leading-none font-semibold whitespace-nowrap text-surface shadow" style={{ left: x(start), top: -6 }}>
      {formatSpan(start, end)} · {end - start + 1}d
    </span>
  );

  if (!shown) {
    // Unscheduled: draw a range to set its dates.
    const a = createDrag ? Math.min(createDrag.anchor, createDrag.current) : undefined;
    const b = createDrag ? Math.max(createDrag.anchor, createDrag.current) : undefined;
    return (
      <div
        className={cn("absolute inset-0", editable && "cursor-cell")}
        onPointerDown={editable ? onCreatePointerDown : undefined}
        onPointerMove={(e) => editable && !drag && onHint(dayAtClient(e.clientX))}
        onPointerLeave={() => onHint(null)}
      >
        {createDrag && a != null && b != null ? (
          <>
            <span className="absolute rounded-[5px] border-[1.5px] border-dashed border-accent bg-accent-soft/60" style={{ left: x(a), width: (b - a + 1) * ppd, top: BAR_TOP, height: BAR_H }} />
            {chip(a, b)}
          </>
        ) : (
          hint != null && (
            <>
              <span className="pointer-events-none absolute rounded-[5px] border border-dashed border-line-strong" style={{ left: x(hint), width: Math.max(ppd, 6), top: BAR_TOP, height: BAR_H }} />
              <span className="pointer-events-none absolute text-[11px] whitespace-nowrap text-muted" style={{ left: x(hint) + Math.max(ppd, 6) + 6, top: BAR_TOP + 4 }}>
                Drag to schedule
              </span>
            </>
          )
        )}
      </div>
    );
  }
  const left = x(shown.start);
  const width = (shown.end - shown.start + 1) * ppd;
  return (
    <span
      className="contents"
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) onSelect(false);
      }}
    >
      <Bar
        issue={issue}
        schedule={shown}
        left={left}
        width={width}
        progress={issue.type.level >= 1 ? progress : undefined}
        editable={editable}
        selected={selected}
        conflict={conflict}
        dragging={!!barDrag}
        linkTarget={linkTarget}
        onPointerDown={onBarPointerDown}
        onKeyDown={onBarKey}
        onFocus={() => onSelect(true)}
        onHover={onHover}
      />
      {barDrag && chip(shown.start, shown.end)}
      {editable && !drag && <LinkHandle left={left + Math.max(width, 4)} visible={selected} issueKey={issue.key} onPointerDown={onLinkPointerDown} />}
      {selected && !drag && <DatePopover issue={issue} schedule={schedule} left={left} editable={editable} onSave={onSave} onOpen={onOpen} />}
    </span>
  );
}

function HoverCard({
  issue,
  schedule,
  warnings,
  progress,
  left,
  top,
}: {
  issue: Issue;
  schedule?: Schedule;
  warnings?: string[];
  progress?: { done: number; total: number };
  left: number;
  top: number;
}) {
  if (!schedule) return null;
  const days = schedule.end - schedule.start + 1;
  return (
    <div role="tooltip" className="pointer-events-none absolute z-30 w-72 rounded-lg border border-line bg-surface px-3 py-2.5 text-xs shadow-xl" style={{ left, top }}>
      <p className="mb-1 font-medium text-ink">
        <span className="font-mono text-muted">{issue.key}</span> {issue.summary}
      </p>
      <p className="text-muted">
        <span className="font-semibold text-ink tabular-nums">{formatSpan(schedule.start, schedule.end)}</span> · {days} day{days === 1 ? "" : "s"} · {issue.status.name}
      </p>
      {issue.type.level >= 1 && progress && progress.total > 0 && (
        <p className="mt-1 text-muted">
          {progress.done} of {progress.total} child issues done
        </p>
      )}
      {schedule.source !== "own" && <p className="mt-1.5 rounded bg-surface-2 px-2 py-1 text-muted">{schedule.note}</p>}
      {warnings?.map((w) => (
        <p key={w} className="mt-1.5 rounded bg-danger-soft px-2 py-1 text-danger">
          {w}
        </p>
      ))}
    </div>
  );
}
