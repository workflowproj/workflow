import { useId, useState, type ReactNode } from "react";
import { Timer, TriangleAlert } from "lucide-react";
import type { IssueDetail } from "@/api/types";
import { Button, useToast } from "@/components/ui";
import { cn } from "@/lib/cn";
import { parseDuration } from "@/lib/duration";
import { useIssue } from "@/features/issues/issue-context";
import { InlineText } from "@/features/issues/inline-edit";
import { useFormatDuration, useTimeUnits } from "./api";
import { LogWorkDialog } from "./LogWorkDialog";
import { barSegments, timeBar, type BarSegment } from "./model";

const segmentClass: Record<BarSegment["kind"], string> = {
  logged: "bg-progress",
  "logged-over": "bg-danger",
  remaining: "bg-line-strong",
  "remaining-over": "bg-danger/35",
};

/**
 * TimeTrackingPanel is the issue's time tracking: a bar of time logged and remaining against
 * the original estimate (overruns in red), the estimates (editable in place) and Log work.
 */
export function TimeTrackingPanel() {
  const { issue, update } = useIssue();
  const units = useTimeUnits();
  const fmt = useFormatDuration();
  const toast = useToast();
  const [logging, setLogging] = useState(false);
  const id = useId();
  const tt = issue.timeTracking ?? { spentSeconds: 0 };
  const canEdit = issue.permissions.edit;

  const commitEstimate = (field: "originalEstimate" | "remainingEstimate") => (text: string) => {
    let seconds: number | null = null;
    if (text) {
      const r = parseDuration(text, units);
      if (r.error !== undefined) {
        toast.error(new Error(r.error));
        return;
      }
      seconds = r.seconds;
    }
    const next = { ...tt };
    if (field === "originalEstimate") {
      next.originalSeconds = seconds ?? undefined;
      // Like the server: a first original estimate also sets the remaining estimate.
      if (seconds != null && tt.remainingSeconds == null) next.remainingSeconds = seconds;
      update({ originalEstimate: seconds }, { timeTracking: next });
    } else {
      next.remainingSeconds = seconds ?? undefined;
      update({ remainingEstimate: seconds }, { timeTracking: next });
    }
  };

  return (
    <section aria-labelledby={id} className="rounded-lg border border-line bg-surface">
      <header className="flex items-center justify-between gap-2 border-b border-line px-4 py-2">
        <h2 id={id} className="text-sm font-semibold">
          Time tracking
        </h2>
        {canEdit && (
          <Button size="sm" variant="subtle" icon={<Timer className="size-3.5" />} onClick={() => setLogging(true)}>
            Log work
          </Button>
        )}
      </header>
      <div className="px-4 py-3">
        <TimeTrackingBar tt={tt} fmt={fmt} />
        <dl className="mt-3 flex flex-col gap-0.5">
          <Row label="Original estimate">
            <InlineText label="Original estimate" value={tt.originalSeconds != null ? fmt(tt.originalSeconds) : ""} placeholder="e.g. 1d 4h" disabled={!canEdit} onCommit={commitEstimate("originalEstimate")} />
          </Row>
          <Row label="Remaining">
            <InlineText label="Remaining estimate" value={tt.remainingSeconds != null ? fmt(tt.remainingSeconds) : ""} placeholder="e.g. 6h" disabled={!canEdit} onCommit={commitEstimate("remainingEstimate")} />
          </Row>
          <Row label="Time logged">
            <p className="flex min-h-8 items-center text-sm tabular-nums">{tt.spentSeconds > 0 ? fmt(tt.spentSeconds) : <span className="text-faint">None</span>}</p>
          </Row>
        </dl>
      </div>
      <LogWorkDialog issue={issue} open={logging} onClose={() => setLogging(false)} />
    </section>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[7.5rem_minmax(0,1fr)] items-start gap-x-3">
      <dt className="truncate pt-1.5 text-[13px] text-muted">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

/** TimeTrackingBar draws logged and remaining time against the estimate, with a caption. */
export function TimeTrackingBar({ tt, fmt }: { tt: NonNullable<IssueDetail["timeTracking"]>; fmt: (s: number | undefined) => string }) {
  const bar = timeBar(tt);
  if (bar.empty) return <p className="text-[13px] text-faint">No estimate or work logged yet.</p>;
  const remaining = tt.remainingSeconds;
  const label = [
    tt.originalSeconds != null && `Estimated ${fmt(tt.originalSeconds)}`,
    `logged ${fmt(tt.spentSeconds)}`,
    remaining != null && `remaining ${fmt(remaining)}`,
    bar.over > 0 && `${fmt(bar.over)} over the estimate`,
  ]
    .filter(Boolean)
    .join(", ");
  return (
    <div>
      <div role="img" aria-label={label} className="relative h-2 overflow-hidden rounded-full bg-surface-3">
        {barSegments(bar).map((s) => (
          <span key={s.kind} className={cn("absolute inset-y-0", segmentClass[s.kind])} style={{ left: `${s.from * 100}%`, width: `${(s.to - s.from) * 100}%` }} />
        ))}
        {bar.estimateAt != null && <span aria-hidden className="absolute inset-y-0 w-0.5 -translate-x-1/2 bg-ink" style={{ left: `${bar.estimateAt * 100}%` }} />}
      </div>
      <div className="mt-1.5 flex justify-between gap-2 text-xs tabular-nums">
        <span className={cn("font-medium", bar.over > 0 && tt.originalSeconds != null && tt.spentSeconds > tt.originalSeconds ? "text-danger" : "text-progress")}>{fmt(tt.spentSeconds)} logged</span>
        <span className="text-muted">{remaining != null ? `${fmt(remaining)} remaining` : "No remaining estimate"}</span>
      </div>
      {bar.over > 0 && (
        <p className="mt-1.5 flex items-center gap-1.5 text-xs font-medium text-danger">
          <TriangleAlert className="size-3.5 shrink-0" aria-hidden />
          {fmt(bar.over)} over the {fmt(tt.originalSeconds)} estimate
        </p>
      )}
    </div>
  );
}
