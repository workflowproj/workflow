import { useCallback, useState, type ReactNode } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { CalendarDays, ExternalLink, GitCompareArrows, Pencil, Rocket, Tag } from "lucide-react";
import type { StatusCategory } from "@/api/types";
import { Badge, Button, Card, PageHeader, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueDrawer } from "@/features/issues/IssueDrawer";
import { formatDay, formatRange, todayISO } from "@/features/backlog/dates";
import { formatPoints } from "@/features/board/model";
import { categoryColor, Segmented } from "@/features/reports/ChartBits";
import { ReadinessSummary } from "@/features/releases/Readiness";
import { ReleaseFormDialog, ShipReleaseDialog } from "@/features/releases/ReleaseDialogs";
import { ReleaseIssues } from "@/features/releases/ReleaseIssues";
import { ReleaseNotes } from "@/features/releases/ReleaseNotes";
import { burnupCaption, ReleaseBurnupChart, type BurnupMeasure } from "@/features/releases/ReleaseBurnupChart";
import { burnupQuery, dateStatus, readinessQuery, releaseQuery, releasesQuery, type ReleaseDetail } from "@/features/releases/queries";
import { formatDate, formatDateTime, relativeTime } from "@/lib/format";

const route = getRouteApi("/app/projects/$projectKey/releases/$releaseId");

/** ReleasePage is one release: its dates, progress, readiness, burnup, issues and notes. */
export function ReleasePage() {
  const { projectKey, releaseId } = route.useParams();
  const { issue } = route.useSearch();
  const navigate = route.useNavigate();
  const openIssue = useCallback((key?: string) => navigate({ search: (s) => ({ ...s, issue: key }) }), [navigate]);
  const { data: release, isLoading, error } = useQuery(releaseQuery(releaseId));
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-5 p-4 md:p-6">
      {isLoading ? <PageSpinner label="Loading release" /> : error || !release ? <ErrorBox error={error ?? new Error("Release not found")} /> : <Release release={release} projectKey={projectKey} onOpen={openIssue} />}
      <IssueDrawer issueKey={issue} onClose={() => openIssue(undefined)} onNavigate={openIssue} />
    </div>
  );
}

type Dialog = "edit" | "ship" | null;

function Release({ release: r, projectKey, onOpen }: { release: ReleaseDetail; projectKey: string; onOpen: (key: string) => void }) {
  const [dialog, setDialog] = useState<Dialog>(null);
  const { data: releases = [] } = useQuery(releasesQuery(r.projectKey));
  const readiness = useQuery(readinessQuery(r.id));
  const status = dateStatus(r, todayISO());
  return (
    <>
      <PageHeader
        breadcrumbs={
          <nav aria-label="Breadcrumb">
            <Link to="/projects/$projectKey/releases" params={{ projectKey }} className="hover:text-ink hover:underline">
              Releases
            </Link>
            <span aria-hidden className="mx-1.5 text-faint">
              /
            </span>
            <span aria-current="page">{r.name}</span>
          </nav>
        }
        title={
          <span className="inline-flex flex-wrap items-center gap-2.5">
            {r.name}
            <StateBadge state={r.state} />
          </span>
        }
        subtitle={r.description || undefined}
        actions={
          r.canManage && (
            <>
              <Button icon={<Pencil className="size-4" />} onClick={() => setDialog("edit")}>
                Edit
              </Button>
              {r.state === "unreleased" && (
                <Button variant="primary" icon={<Rocket className="size-4" />} onClick={() => setDialog("ship")}>
                  Release
                </Button>
              )}
            </>
          )
        }
      />
      <div className="-mt-2 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted">
        <span className="inline-flex items-center gap-1.5">
          <CalendarDays aria-hidden className="size-4" />
          {r.startDate || r.releaseDate ? (r.startDate ? formatRange(r.startDate, r.releaseDate) : `Due ${formatDay(r.releaseDate!)}`) : "No dates"}
        </span>
        {status && <span className={status.late ? "font-medium text-danger" : "font-medium text-ink"}>{status.text}</span>}
        {r.releasedAt && <span title={formatDateTime(r.releasedAt)}>Released {formatDate(r.releasedAt)}</span>}
        <TagLine release={r} onLink={() => setDialog("edit")} />
      </div>
      <Environments release={r} />

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Progress">
          <ReleaseProgress release={r} />
        </Card>
        <Card title="Readiness" actions={readiness.data && !readiness.data.ready && <Badge tone="warning">{readiness.data.warnings.length} to check</Badge>}>
          <div className="p-4">
            {readiness.isLoading ? <PageSpinner label="Checking the release" /> : readiness.error ? <ErrorBox error={readiness.error} /> : readiness.data && <ReadinessSummary readiness={readiness.data} onOpen={onOpen} />}
          </div>
        </Card>
      </div>
      <Burnup release={r} />
      <Card title="Issues">
        <ReleaseIssues projectKey={r.projectKey} release={r} onOpen={onOpen} filters />
      </Card>
      <Card title="Release notes">
        <ReleaseNotes release={r} />
      </Card>

      <ReleaseFormDialog open={dialog === "edit"} onClose={() => setDialog(null)} release={r} projectKey={r.projectKey} />
      <ShipReleaseDialog
        open={dialog === "ship"}
        onClose={() => setDialog(null)}
        release={r}
        others={releases.filter((x) => x.state === "unreleased" && x.id !== r.id)}
        openCount={r.issueCount - r.doneCount}
      />
    </>
  );
}

function StateBadge({ state }: { state: ReleaseDetail["state"] }) {
  if (state === "released") return <Badge tone="success">Released</Badge>;
  if (state === "archived") return <Badge>Archived</Badge>;
  return <Badge tone="accent">Unreleased</Badge>;
}

/** TagLine shows the release's Git tag, with links to it and to the changes since the previous one. */
function TagLine({ release: r, onLink }: { release: ReleaseDetail; onLink: () => void }) {
  const t = r.tag;
  if (!t)
    return r.canManage ? (
      <button type="button" onClick={onLink} className="inline-flex items-center gap-1.5 text-accent hover:underline">
        <Tag aria-hidden className="size-4" /> Link a Git tag
      </button>
    ) : null;
  return (
    <span className="inline-flex flex-wrap items-center gap-x-3 gap-y-1">
      <a href={t.url} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1.5 hover:text-accent" title={`${t.repository.fullPath}${t.sha ? ` at ${t.sha.slice(0, 10)}` : ""}`}>
        <Tag aria-hidden className="size-4" />
        <span className="font-mono text-[13px] text-ink">{t.name}</span>
        <span className="text-xs">{t.repository.fullPath}</span>
        <ExternalLink aria-hidden className="size-3.5" />
      </a>
      {t.compareUrl && (
        <a href={t.compareUrl} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-1.5 hover:text-accent">
          <GitCompareArrows aria-hidden className="size-4" /> Changes since {t.previousTag}
        </a>
      )}
    </span>
  );
}

/** Environments lists where the release has been deployed: by its tag, or because all its done issues were. */
function Environments({ release: r }: { release: ReleaseDetail }) {
  if (r.environments.length === 0) return null;
  return (
    <section aria-label="Environments" className="flex flex-wrap items-center gap-2">
      <span className="text-[13px] text-muted">Reached</span>
      {r.environments.map((e) => {
        const why = e.via === "tag" ? `Its tag was deployed to ${e.environment}` : `Every done issue was deployed to ${e.environment}`;
        const body = (
          <>
            <Rocket aria-hidden className="size-3.5 text-success" />
            <span className="font-medium text-ink">{e.environment}</span>
            <span className="text-muted">{relativeTime(e.reachedAt)}</span>
          </>
        );
        const cls = "inline-flex items-center gap-1.5 rounded-full border border-line bg-surface px-2.5 py-0.5 text-[13px]";
        return e.url ? (
          <a key={e.environment} href={e.url} target="_blank" rel="noreferrer noopener" className={`${cls} hover:border-accent`} title={`${why}, ${formatDateTime(e.reachedAt)}`}>
            {body}
          </a>
        ) : (
          <span key={e.environment} className={cls} title={`${why}, ${formatDateTime(e.reachedAt)}`}>
            {body}
          </span>
        );
      })}
    </section>
  );
}

const SEGMENTS: { key: "done" | "inProgress" | "todo"; label: string; category: StatusCategory }[] = [
  { key: "done", label: "done", category: "done" },
  { key: "inProgress", label: "in progress", category: "in_progress" },
  { key: "todo", label: "to do", category: "todo" },
];

/** ReleaseProgress shows issues and story points by status category, as bars and numbers. */
function ReleaseProgress({ release: r }: { release: ReleaseDetail }) {
  const { issues, points, unestimated } = r.progress;
  return (
    <div className="flex flex-col gap-5 p-4">
      <ProgressBar
        label="Issues"
        counts={issues}
        format={(n) => String(n)}
        note={issues.total === 0 ? "No issues have this release as their fix version yet." : undefined}
      />
      <ProgressBar
        label="Story points"
        counts={points}
        format={formatPoints}
        note={unestimated > 0 ? `${unestimated} issue${unestimated === 1 ? " has" : "s have"} no story points.` : undefined}
      />
    </div>
  );
}

function ProgressBar({ label, counts, format, note }: { label: string; counts: ReleaseDetail["progress"]["issues"]; format: (n: number) => string; note?: ReactNode }) {
  const pct = (n: number) => (counts.total ? (n / counts.total) * 100 : 0);
  return (
    <div>
      <div className="mb-1.5 flex items-baseline justify-between gap-3">
        <h3 className="text-[13px] font-medium">{label}</h3>
        <span className="text-sm text-muted tabular-nums">
          <span className="font-semibold text-ink">{format(counts.done)}</span> of {format(counts.total)} done ({Math.round(pct(counts.done))}%)
        </span>
      </div>
      <div
        role="img"
        aria-label={`${label}: ${SEGMENTS.map((s) => `${format(counts[s.key])} ${s.label}`).join(", ")}`}
        className="flex h-2.5 overflow-hidden rounded-full bg-surface-3"
      >
        {SEGMENTS.map((s) => (
          <div key={s.key} className="h-full transition-[width]" style={{ width: `${pct(counts[s.key])}%`, background: categoryColor[s.category] }} />
        ))}
      </div>
      <ul className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        {SEGMENTS.map((s) => (
          <li key={s.key} className="inline-flex items-center gap-1.5">
            <span aria-hidden className="size-2.5 rounded-[3px]" style={{ background: categoryColor[s.category] }} />
            <span className="font-medium text-ink tabular-nums">{format(counts[s.key])}</span> {s.label}
          </li>
        ))}
      </ul>
      {note && <p className="mt-1.5 text-xs text-muted">{note}</p>}
    </div>
  );
}

/** Burnup charts the release's done work against its scope, in issues or story points. */
function Burnup({ release: r }: { release: ReleaseDetail }) {
  const { data, isLoading, error } = useQuery(burnupQuery(r.id));
  const [measure, setMeasure] = useState<BurnupMeasure | null>(null);
  const m: BurnupMeasure = measure ?? (r.progress.points.total > 0 ? "points" : "issues");
  return (
    <Card
      title="Burnup"
      actions={
        <Segmented
          label="Measure"
          value={m}
          onChange={setMeasure}
          options={[
            { value: "issues", label: "Issues" },
            { value: "points", label: "Story points" },
          ]}
        />
      }
    >
      <figure className="p-4">
        {isLoading ? <PageSpinner label="Loading the burnup" /> : error ? <ErrorBox error={error} /> : data && <ReleaseBurnupChart burnup={data} measure={m} />}
        {data && <figcaption className="mt-2 text-xs text-muted">{burnupCaption(data)}</figcaption>}
      </figure>
    </Card>
  );
}
