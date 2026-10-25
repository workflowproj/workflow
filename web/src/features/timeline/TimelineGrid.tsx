import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import type { Issue } from "@/api/types";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Background } from "./Background";
import { ROW_H, type DragMode } from "./Bar";
import { Dependencies } from "./Dependencies";
import { preview, type Drag } from "./drag";
import { GridRow, type Markers, type RowActions } from "./GridRow";
import { MarkerLane } from "./Markers";
import { formatSpan, fromDay, isOverdue, toDay, type Dependency, type Row, type Schedule, type TimelineData, type TimelineLink } from "./model";
import { LinkMenu } from "./Popovers";
import { ppdFor, snapDays, timeRange, type Zoom } from "./scale";
import { BAND_H, LANE_H, TimeHeader } from "./TimeHeader";
import { TREE_W } from "./TreeCell";

const OVERSCAN = 8;

export type TimelineEdits = {
  schedule: (issue: Issue, start: string | null, due: string | null) => void;
  link: (source: string, target: string) => void;
  unlink: (l: TimelineLink) => void;
};

/**
 * TimelineGrid is the scrolling tree + time grid. Rows are windowed and memoised, so a few
 * hundred issues stay smooth. Bars can be dragged (move, resize, draw on an empty row),
 * dependencies drawn from a bar's end handle onto another bar, and a focused bar shows a
 * date popover.
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
  const span = useMemo(() => {
    const days: number[] = [];
    for (const s of sched.values()) days.push(s.start, s.end);
    for (const { sprint } of data.sprints) for (const d of [sprint.startDate, sprint.endDate]) if (d) days.push(toDay(d));
    for (const { release } of data.releases) if (release.releaseDate) days.push(toDay(release.releaseDate));
    return timeRange(days, today);
  }, [sched, data.sprints, data.releases, today]);
  // The grid always fills the viewport, however short the plan.
  const [viewW, setViewW] = useState(1200);
  const range = useMemo(() => ({ start: span.start, end: Math.max(span.end, span.start + Math.ceil(viewW / ppd)) }), [span, viewW, ppd]);
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
  const markers = useMemo(() => {
    const m = new Map<string, Markers>(data.projects.map((p) => [p.key, { sprints: [], releases: [] }]));
    for (const s of data.sprints) m.get(s.projectKey)?.sprints.push(s.sprint);
    for (const r of data.releases) m.get(r.projectKey)?.releases.push(r.release);
    return m;
  }, [data.projects, data.sprints, data.releases]);
  const summaries = useMemo(() => new Map(data.issues.map((i) => [i.key, i.summary])), [data.issues]);

  // Scrolling and windowing.
  const scroller = useRef<HTMLDivElement>(null);
  const [win, setWin] = useState({ first: 0, last: 40 });
  const measure = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    setViewW(el.clientWidth - TREE_W);
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

  // Keep the date in the middle in place when the zoom or the range start changes.
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
  // On mount and when asked ("Today"), bring today into view.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el) el.scrollLeft = x(today) - (el.clientWidth - TREE_W) * 0.3;
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

  /** point converts a pointer position to grid coordinates (x from the first day, y from the first row). */
  const point = (clientX: number, clientY: number) => {
    const el = scroller.current!;
    const box = el.getBoundingClientRect();
    return { x: clientX - box.left + el.scrollLeft - TREE_W, y: clientY - box.top + el.scrollTop - headerH };
  };
  const dayAt = (clientX: number) => range.start + Math.floor(point(clientX, 0).x / ppd);
  // Dragging towards an edge of the grid scrolls it.
  const autoScroll = (e: PointerEvent, vertical: boolean) => {
    const el = scroller.current!;
    const box = el.getBoundingClientRect();
    if (e.movementX > 0 && e.clientX > box.right - 28) el.scrollLeft += 12;
    else if (e.movementX < 0 && e.clientX < box.left + TREE_W + 28) el.scrollLeft -= 12;
    if (vertical) {
      if (e.movementY > 0 && e.clientY > box.bottom - 28) el.scrollTop += 12;
      else if (e.movementY < 0 && e.clientY < box.top + headerH + 28) el.scrollTop -= 12;
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
        setDrag({ ...d, current: dayAt(e.clientX) });
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
        if (d.current !== d.anchor) edits.schedule(d.issue, fromDay(Math.min(d.anchor, d.current)), fromDay(Math.max(d.anchor, d.current)));
      } else if (d.target) edits.link(d.from, d.target);
    };
    const onCancel = () => setDrag(null);
    const onKey = (e: globalThis.KeyboardEvent) => e.key === "Escape" && setDrag(null);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onCancel);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onCancel);
      window.removeEventListener("keydown", onKey);
    };
    // The handlers read the live drag from the ref; they only need binding once per drag.
  }, [dragging]);

  // Up and down move between bars (scrolling as needed).
  const focusBar = (key: string) => {
    const i = rowOf.get(key);
    const el = scroller.current;
    if (i == null || !el) return;
    if (i * ROW_H < el.scrollTop || (i + 1) * ROW_H > el.scrollTop + el.clientHeight - headerH) el.scrollTop = i * ROW_H - (el.clientHeight - headerH) / 2;
    requestAnimationFrame(() => requestAnimationFrame(() => el.querySelector<HTMLElement>(`[data-bar-key="${key}"]`)?.focus()));
  };

  // Rows get one stable set of callbacks that read the latest state through a ref.
  const live = {
    toggle: onToggle,
    open: onOpen,
    startBar: (e: ReactPointerEvent, issue: Issue, mode: DragMode) => {
      const base = sched.get(issue.key);
      if (e.button !== 0 || !base) return;
      e.preventDefault(); // no text selection, and no focus: focus is the keyboard path
      setLinkMenu(null);
      setDrag({ kind: "bar", mode, issue, base, clientX0: e.clientX, scroll0: scroller.current!.scrollLeft, delta: 0, moved: false, editable: canEdit(issue.projectKey) });
    },
    startLink: (e: ReactPointerEvent, issue: Issue, rowIndex: number) => {
      const s = sched.get(issue.key);
      if (e.button !== 0 || !s) return;
      e.preventDefault();
      e.stopPropagation();
      const x0 = x(s.end + 1);
      const y0 = rowIndex * ROW_H + ROW_H / 2;
      setDrag({ kind: "link", from: issue.key, x0, y0, x: x0, y: y0 });
    },
    startCreate: (e: ReactPointerEvent, issue: Issue) => {
      if (e.button !== 0) return;
      e.preventDefault();
      const day = dayAt(e.clientX);
      setDrag({ kind: "create", issue, anchor: day, current: day });
    },
    hint: (key: string, clientX: number | null) => setHint(clientX == null ? null : { key, day: dayAt(clientX) }),
    barKey: (e: KeyboardEvent, issue: Issue) => {
      if (e.key === "Enter") {
        e.preventDefault();
        onOpen(issue.key);
      } else if (e.key === "Escape") (e.currentTarget as HTMLElement).blur();
      else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const step = e.key === "ArrowDown" ? 1 : -1;
        for (let i = (rowOf.get(issue.key) ?? 0) + step; i >= 0 && i < rows.length; i += step) {
          const r = rows[i];
          if (r.kind === "issue" && sched.has(r.id)) return focusBar(r.id);
        }
      }
    },
    select: (key: string, on: boolean) => setSelected((s) => (on ? key : s === key ? null : s)),
    hover: (key: string, on: boolean) => setHover((h) => (on ? key : h === key ? null : h)),
    save: (issue: Issue, start: string | null, due: string | null) => edits.schedule(issue, start, due),
  };
  const liveRef = useRef(live);
  liveRef.current = live;
  const actions = useMemo<RowActions>(
    () => ({
      toggle: (id) => liveRef.current.toggle(id),
      open: (key) => liveRef.current.open(key),
      startBar: (e, issue, mode) => liveRef.current.startBar(e, issue, mode),
      startLink: (e, issue, i) => liveRef.current.startLink(e, issue, i),
      startCreate: (e, issue) => liveRef.current.startCreate(e, issue),
      hint: (key, cx) => liveRef.current.hint(key, cx),
      barKey: (e, issue) => liveRef.current.barKey(e, issue),
      select: (key, on) => liveRef.current.select(key, on),
      hover: (key, on) => liveRef.current.hover(key, on),
      save: (issue, s, d) => liveRef.current.save(issue, s, d),
    }),
    [],
  );
  const onSelectLink = useCallback(
    (link: TimelineLink, at: { clientX: number; clientY: number }) => {
      const el = scroller.current!;
      const box = el.getBoundingClientRect();
      const px = at.clientX - box.left + el.scrollLeft - TREE_W;
      const py = at.clientY - box.top + el.scrollTop - headerH;
      setLinkMenu({ link, left: TREE_W + Math.min(px + 8, gridW - 290), top: py + 8 });
    },
    [headerH, gridW],
  );

  const lane = useMemo(() => {
    const m = !multi && data.projects[0] ? markers.get(data.projects[0].key) : undefined;
    return m ? <MarkerLane sprints={m.sprints} releases={m.releases} x={x} ppd={ppd} today={today} height={LANE_H} /> : undefined;
  }, [multi, data.projects, markers, x, ppd, today]);
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
          <TimeHeader range={range} zoom={zoom} x={x} ppd={ppd} width={gridW} today={today} lane={lane} />
        </div>

        <div className="absolute left-0" style={{ top: headerH, width: TREE_W + gridW, height: bodyH }}>
          <div className="absolute top-0" style={{ left: TREE_W, width: gridW, height: bodyH }}>
            <Background range={range} zoom={zoom} x={x} ppd={ppd} width={gridW} height={bodyH} spans={spans} markers={markers} today={today} />
            <Dependencies deps={deps} rowOf={rowOf} x={x} width={gridW} height={bodyH} selected={linkMenu?.link.id} onSelect={onSelectLink} />
          </div>
          <div aria-hidden className="sticky left-0 z-10 h-full border-r border-line bg-surface" style={{ width: TREE_W }} />

          {rows.slice(win.first, win.last + 1).map((row, k) => {
            const i = win.first + k;
            const key = row.id;
            return (
              <GridRow
                key={key}
                row={row}
                index={i}
                gridW={gridW}
                x={x}
                ppd={ppd}
                today={today}
                collapsed={collapsed.has(key)}
                warnings={row.kind === "issue" ? warnings.get(key) : undefined}
                overdue={row.kind === "issue" && isOverdue(row.issue, today)}
                schedule={row.kind === "issue" ? sched.get(key) : undefined}
                editable={row.kind !== "project" && canEdit(row.projectKey)}
                progress={row.kind === "issue" ? progress?.get(key) : undefined}
                selected={selected === key}
                hint={hint?.key === key ? hint.day : undefined}
                barDrag={drag?.kind === "bar" && drag.issue.key === key ? drag : null}
                createDrag={drag?.kind === "create" && drag.issue.key === key ? drag : null}
                linkTarget={drag?.kind === "link" && drag.target === key}
                dragging={dragging}
                markers={row.kind === "project" ? markers.get(row.project.key) : undefined}
                actions={actions}
              />
            );
          })}

          {drag?.kind === "link" && (
            <svg aria-hidden className="pointer-events-none absolute top-0 z-[7] overflow-visible" style={{ left: TREE_W }} width={gridW} height={bodyH}>
              <path d={`M${drag.x0},${drag.y0}C${drag.x0 + 40},${drag.y0} ${drag.x - 40},${drag.y} ${drag.x},${drag.y}`} fill="none" stroke="var(--color-accent)" strokeWidth={2} strokeDasharray="5 4" />
              <circle cx={drag.x} cy={drag.y} r={4} fill="var(--color-accent)" />
            </svg>
          )}
          {hovered && (
            <HoverCard
              issue={hovered}
              schedule={sched.get(hovered.key)}
              warnings={warnings.get(hovered.key)}
              progress={progress?.get(hovered.key)}
              left={TREE_W + Math.max(8, x(sched.get(hovered.key)?.start ?? range.start))}
              top={((rowOf.get(hovered.key) ?? 0) + 1) * ROW_H - 4}
            />
          )}
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

/** HoverCard explains a bar: dates, length, status, progress, why its dates are derived and any conflicts. */
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
