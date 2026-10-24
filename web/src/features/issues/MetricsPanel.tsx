import { useId, useState, type CSSProperties } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, RotateCcw } from "lucide-react";
import { ApiError } from "@/api/client";
import type { StatusCategory } from "@/api/types";
import { inStatus } from "@/features/board/card-fields";
import { formatDateTime } from "@/lib/format";
import { cn } from "@/lib/cn";
import { metricsQuery } from "./issue-api";
import { useIssue } from "./issue-context";
import { formatHours, statusSegments, type IssueMetrics, type Segment } from "./metrics";

// Status-category fills; to-do uses the faint grey so it stays apart from progress blue in dark mode too.
const fill: Record<StatusCategory, string> = { todo: "bg-faint", in_progress: "bg-progress", done: "bg-done" };
// A second status of the same category next to the first gets a diagonal texture.
const texture: CSSProperties = { backgroundImage: "repeating-linear-gradient(135deg, color-mix(in srgb, var(--color-surface) 55%, transparent) 0 2px, transparent 2px 5px)" };

const STORAGE = "wfm.issue.metrics";

function readOpen() {
  try {
    return localStorage.getItem(STORAGE) !== "closed";
  } catch {
    return true;
  }
}

/**
 * MetricsPanel shows how an issue has moved: lead and cycle time, age, time in the current
 * status, reopen and sprint counts, and a bar of the time spent in each status. It can be
 * collapsed (remembered per browser).
 */
export function MetricsPanel() {
  const { issue } = useIssue();
  const [open, setOpen] = useState(readOpen);
  const { data, isLoading, error, refetch, isPlaceholderData } = useQuery({ ...metricsQuery(issue.key, issue.version), enabled: open });
  const id = useId();
  const toggle = () => {
    setOpen(!open);
    try {
      localStorage.setItem(STORAGE, open ? "closed" : "open");
    } catch {
      // Storage can be unavailable; the choice then lasts for this visit.
    }
  };
  return (
    <section aria-labelledby={id} className="@container rounded-lg border border-line bg-surface">
      <h2 id={id} className={cn("text-sm font-semibold", open && "border-b border-line")}>
        <button type="button" aria-expanded={open} onClick={toggle} className="flex w-full items-center gap-1.5 rounded-lg px-4 py-2.5 text-left hover:bg-surface-2/50 focus-visible:outline-2 focus-visible:outline-accent">
          <ChevronRight className={cn("size-4 text-faint transition-transform", open && "rotate-90")} />
          Metrics
        </button>
      </h2>
      {!open ? null : isLoading ? (
        <Loading />
      ) : data ? (
        <MetricsBody m={data} stale={isPlaceholderData} />
      ) : (
        <p className="px-4 py-3 text-[13px] text-muted">
          {error instanceof ApiError && error.status === 404 ? (
            "Metrics aren't available for this issue."
          ) : (
            <>
              Metrics couldn't be loaded.{" "}
              <button type="button" onClick={() => refetch()} className="font-medium text-accent hover:underline">
                Try again
              </button>
            </>
          )}
        </p>
      )}
    </section>
  );
}

function Loading() {
  return (
    <div aria-busy="true" aria-label="Loading metrics" className="flex animate-pulse flex-col gap-3 px-4 py-3">
      <div className="grid grid-cols-2 gap-3 @md:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-9 rounded bg-surface-2" />
        ))}
      </div>
      <div className="h-2.5 rounded bg-surface-2" />
    </div>
  );
}

function MetricsBody({ m, stale }: { m: IssueMetrics; stale: boolean }) {
  const { issue } = useIssue();
  const resolved = !!m.resolvedAt;
  const cycle =
    m.cycleTimeHours == null
      ? { label: "Cycle time", value: null, title: resolved ? "Never in progress" : "Not started yet" }
      : resolved
        ? { label: "Cycle time", value: formatHours(m.cycleTimeHours), title: "First in progress to resolved" }
        : { label: "In progress for", value: formatHours(m.cycleTimeHours), title: `First in progress ${formatDateTime(m.startedAt)}` };
  const tiles = [
    { label: "Lead time", value: m.leadTimeHours != null ? formatHours(m.leadTimeHours) : null, title: m.leadTimeHours != null ? "Created to resolved" : "Not resolved yet" },
    cycle,
    { label: "Age", value: formatHours(m.ageHours), title: resolved ? "Created to resolved" : `Created ${formatDateTime(m.createdAt)}` },
    { label: `${inStatus(issue.status.name)} for`, value: formatHours(m.statusAgeHours), title: `Since ${formatDateTime(m.statusSince)}` },
  ];
  return (
    <div className={cn("flex flex-col gap-3 px-4 py-3 transition-opacity", stale && "opacity-60")}>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2.5 @md:grid-cols-4">
        {tiles.map((t) => (
          <div key={t.label} className="min-w-0" title={t.title}>
            <dt className="truncate text-xs text-muted">{t.label}</dt>
            <dd className={cn("text-sm font-semibold tabular-nums", !t.value && "font-normal text-faint")}>{t.value ?? (t.title === "Not started yet" ? "Not started" : "—")}</dd>
          </div>
        ))}
      </dl>
      {(m.reopened > 0 || m.sprints > 0) && (
        <p className="flex flex-wrap gap-1.5 text-xs">
          {m.reopened > 0 && (
            <span className="inline-flex items-center gap-1 rounded bg-surface-2 px-1.5 py-0.5 text-muted" title="Times it left a done status again">
              <RotateCcw className="size-3 text-warning" aria-hidden />
              Reopened {m.reopened === 1 ? "once" : `${m.reopened} times`}
            </span>
          )}
          {m.sprints > 0 && (
            <span className="rounded bg-surface-2 px-1.5 py-0.5 text-muted" title="Sprints it has been part of">
              In {m.sprints} sprint{m.sprints === 1 ? "" : "s"}
            </span>
          )}
        </p>
      )}
      <TimeInStatus m={m} currentStatusId={issue.status.id} />
    </div>
  );
}

function TimeInStatus({ m, currentStatusId }: { m: IssueMetrics; currentStatusId: string }) {
  const { segments, total, resolved } = statusSegments(m, currentStatusId);
  const [hovered, setHovered] = useState<string | null>(null);
  const id = useId();
  if (segments.length === 0) return null;
  const bar = segments.filter((s) => s.hours > 0);
  const hover = (s: Segment) => ({ onMouseEnter: () => setHovered(s.status.id), onMouseLeave: () => setHovered(null) });
  return (
    <div>
      <div className="mb-1.5 flex items-baseline justify-between gap-2">
        <h3 id={id} className="text-xs font-semibold text-muted">
          Time in status
        </h3>
        <span className="text-[11px] text-faint">{resolved ? "until resolved" : "until now"}</span>
      </div>
      {total > 0 && (
        <div role="img" aria-label={`Time in status: ${bar.map((s) => `${s.status.name} ${formatHours(s.hours)}`).join(", ")}`} className="flex h-2.5 gap-0.5 overflow-hidden rounded">
          {bar.map((s) => (
            <div
              key={s.status.id}
              {...hover(s)}
              title={`${s.status.name}: ${formatHours(s.hours)} (${Math.round(s.share * 100)}%)`}
              style={{ flexGrow: s.share, flexBasis: 0, minWidth: 3, ...(s.alt ? texture : {}) }}
              className={cn("transition-opacity", fill[s.status.category], hovered && hovered !== s.status.id && "opacity-30")}
            />
          ))}
        </div>
      )}
      <ul aria-labelledby={id} className="mt-2 grid gap-x-4 @md:grid-cols-2">
        {segments.map((s) => (
          <li key={s.status.id} {...hover(s)} className={cn("-mx-1 flex min-w-0 items-center gap-2 rounded px-1 py-0.5 text-[13px]", hovered === s.status.id && "bg-surface-2")}>
            <span aria-hidden className={cn("size-2.5 shrink-0 rounded-sm", fill[s.status.category])} style={s.alt ? texture : undefined} />
            <span className="min-w-0 flex-1 truncate">{s.status.name}</span>
            {s.current && <span className="text-[11px] font-medium text-accent">now</span>}
            {s.visits > 1 && (
              <span className="text-[11px] text-faint" title={`Entered ${s.visits} times`}>
                ×{s.visits}
              </span>
            )}
            <span className="shrink-0 text-muted tabular-nums">{formatHours(s.total)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
