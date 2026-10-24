import { useEffect } from "react";
import { Play, RefreshCw } from "lucide-react";
import { Badge, Button, Card, PageSpinner, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout } from "@/features/settings/common";
import { formatBytes } from "@/lib/format";
import { useImportActions } from "./api";
import { plural, problemCounts, type JiraImport } from "./model";
import { ProblemList, Stat } from "./parts";

const templateName: Record<string, string> = { scrum: "Scrum", kanban: "Kanban", bug_tracking: "Bug tracking" };

/** DryRunStep shows what the import would do and the problems it would meet; nothing is written. */
export function DryRunStep({ imp, onStarted }: { imp: JiraImport; onStarted: () => void }) {
  const actions = useImportActions(imp.id);
  const { dryRun } = actions;
  const mutate = dryRun.mutate;
  useEffect(() => {
    mutate();
  }, [mutate, imp.updatedAt]);
  const dry = dryRun.data;
  const counts = problemCounts(dry?.problems ?? []);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">A dry run checks the mapping against the analysis without writing anything.</p>
        <Button size="sm" variant="ghost" icon={<RefreshCw className="size-3.5" />} loading={dryRun.isPending} onClick={() => dryRun.mutate()}>
          Check again
        </Button>
      </div>
      {dryRun.error && <ErrorBox error={dryRun.error} />}
      {!dry ? (
        <PageSpinner label="Checking" />
      ) : (
        <>
          <Card title="What will be imported">
            <div className="overflow-x-auto">
              <table className={table}>
                <thead>
                  <tr>
                    <th>Project</th>
                    <th className="text-right">Issues</th>
                    <th className="text-right">Comments</th>
                    <th className="text-right">Attachments</th>
                    <th className="text-right">Sprints</th>
                    <th className="text-right">Versions</th>
                  </tr>
                </thead>
                <tbody>
                  {dry.projects.map((p) => (
                    <tr key={p.jiraKey}>
                      <td>
                        <span className="font-mono text-xs text-muted">{p.jiraKey}</span> → <span className="font-mono text-sm font-medium">{p.key}</span>{" "}
                        <span className="text-[13px]">{p.name}</span>{" "}
                        {p.existing ? <Badge tone="accent">Update</Badge> : <Badge>{templateName[p.template] ?? p.template}</Badge>}
                      </td>
                      <td className="text-right tabular-nums">{p.issues}</td>
                      <td className="text-right tabular-nums">{p.comments}</td>
                      <td className="text-right tabular-nums">
                        {p.attachments - p.skippedAttachments}
                        <span className="block text-[11px] text-muted">
                          {formatBytes(p.attachmentBytes)}
                          {p.skippedAttachments > 0 && `, ${p.skippedAttachments} skipped`}
                        </span>
                      </td>
                      <td className="text-right tabular-nums">{p.sprints}</td>
                      <td className="text-right tabular-nums">{p.versions}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Stat label="People matched" value={dry.people.matched} hint="existing accounts" />
            <Stat label="Claimable placeholders" value={dry.people.placeholders} hint="claimed by signing in" />
            <Stat label="Unclaimable placeholders" value={dry.people.unclaimable} hint="no email" />
            <Stat label="Left empty" value={dry.people.skipped} />
          </div>
          <Card
            title="Problems"
            actions={
              <span className="flex gap-1.5">
                {counts.error > 0 && <Badge tone="danger">{counts.error} blocking</Badge>}
                {counts.warning > 0 && <Badge tone="warning">{plural(counts.warning, "warning")}</Badge>}
                {counts.info > 0 && <Badge>{plural(counts.info, "note")}</Badge>}
              </span>
            }
          >
            <ProblemList problems={dry.problems} empty="Nothing to worry about." />
          </Card>
          {actions.run.error && <ErrorBox error={actions.run.error} />}
          {dry.ready ? (
            <Callout title="Ready to import">
              The import runs in the background and can be left; it resumes where it stopped if interrupted, and running it again later updates what changed in Jira without
              duplicating anything.
            </Callout>
          ) : (
            <Callout tone="danger" title="Fix the blocking problems first" />
          )}
          <div className="flex justify-end">
            <Button
              variant="primary"
              icon={<Play className="size-4" />}
              disabled={!dry.ready}
              loading={actions.run.isPending}
              onClick={() => actions.run.mutateAsync().then(onStarted, () => undefined)}
            >
              Start import
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
