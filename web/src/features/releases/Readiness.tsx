import { CalendarX2, CircleCheck, CircleDashed, CircleX, ExternalLink, GitPullRequest, type LucideIcon } from "lucide-react";
import type { Schemas } from "@/api/client";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import type { ReleaseReadiness } from "./queries";

type Warning = Schemas["ReadinessWarning"];

const kinds: Record<Warning["kind"], { icon: LucideIcon; title: string }> = {
  not_done: { icon: CircleDashed, title: "Issues not done" },
  open_pull_requests: { icon: GitPullRequest, title: "Open pull requests" },
  failed_pipelines: { icon: CircleX, title: "Failed pipelines" },
  overdue: { icon: CalendarX2, title: "Release date passed" },
};

/** ReadinessSummary lists what stands in the way of shipping, each warning with its issues. */
export function ReadinessSummary({ readiness, onOpen }: { readiness: ReleaseReadiness; onOpen?: (key: string) => void }) {
  if (readiness.ready)
    return (
      <p className="flex items-center gap-2 text-sm text-success">
        <CircleCheck aria-hidden className="size-4" /> Ready to release: every issue is done, with nothing open or failing.
      </p>
    );
  return (
    <ul className="flex flex-col gap-3">
      {readiness.warnings.map((w) => {
        const k = kinds[w.kind];
        return (
          <li key={w.kind} className="rounded-md border border-warning/40 bg-warning-soft/50">
            <p className="flex items-start gap-2 px-3 py-2 text-sm">
              <k.icon aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />
              <span>
                <span className="font-medium">{k.title}.</span> <span className="text-muted">{w.message}</span>
              </span>
            </p>
            {w.issues.length > 0 && (
              <ul className="divide-y divide-line border-t border-warning/30 bg-surface/70">
                {w.issues.map(({ issue, links }) => (
                  <li key={issue.id} className="flex flex-wrap items-center gap-x-2.5 gap-y-1 px-3 py-1.5 text-sm">
                    <IssueTypeIcon type={issue.type} />
                    {onOpen ? (
                      <button type="button" onClick={() => onOpen(issue.key)} className="font-mono text-xs text-muted hover:text-accent hover:underline">
                        {issue.key}
                      </button>
                    ) : (
                      <span className="font-mono text-xs text-muted">{issue.key}</span>
                    )}
                    <span className="min-w-0 flex-1 truncate">{issue.summary}</span>
                    {w.kind === "not_done" && <StatusBadge status={issue.status} className="max-w-[9rem]" />}
                    {links.map((l) => (
                      <a
                        key={l.url}
                        href={l.url}
                        target="_blank"
                        rel="noreferrer noopener"
                        className={cn("inline-flex max-w-[16rem] items-center gap-1 truncate text-xs hover:underline", l.state === "failed" ? "text-danger" : "text-accent")}
                        title={l.title}
                      >
                        {l.repository || l.title}
                        <ExternalLink aria-hidden className="size-3 shrink-0" />
                      </a>
                    ))}
                  </li>
                ))}
              </ul>
            )}
          </li>
        );
      })}
    </ul>
  );
}

/** ReadinessBrief is the short form for the Release dialog: each warning and the issue keys. */
export function ReadinessBrief({ readiness }: { readiness: ReleaseReadiness }) {
  if (readiness.ready)
    return (
      <p className="flex items-center gap-2 text-sm text-success">
        <CircleCheck aria-hidden className="size-4" /> Every issue is done, with nothing open or failing.
      </p>
    );
  return (
    <ul className="flex flex-col gap-1.5 rounded-md border border-warning/40 bg-warning-soft px-3 py-2.5 text-sm">
      {readiness.warnings.map((w) => {
        const k = kinds[w.kind];
        const keys = w.issues.map((i) => i.issue.key);
        return (
          <li key={w.kind} className="flex items-start gap-2">
            <k.icon aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />
            <span>
              {w.message}
              {keys.length > 0 && (
                <span className="text-muted">
                  {" "}
                  ({keys.slice(0, 6).join(", ")}
                  {keys.length > 6 && `, and ${keys.length - 6} more`})
                </span>
              )}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
