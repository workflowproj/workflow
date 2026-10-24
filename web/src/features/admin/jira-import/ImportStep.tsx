import { Link } from "@tanstack/react-router";
import { KeyRound, Play, RotateCcw, Square } from "lucide-react";
import { Badge, Button, Card, useConfirm } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout, When } from "@/features/settings/common";
import { useImportActions } from "./api";
import { busy, overallPercent, plural, problemCounts, reportRows, STATUS_LABEL, STATUS_TONE, type JiraImport } from "./model";
import { ProblemList, ProgressBar, Stat } from "./parts";
import { Phases } from "./ProgressPanel";

/** ImportStep follows the running import and shows the final report. */
export function ImportStep({ imp }: { imp: JiraImport }) {
  const actions = useImportActions(imp.id);
  const confirm = useConfirm();
  const running = busy(imp);
  const progress = imp.progress;
  const rows = reportRows(progress?.counters);
  const counts = problemCounts(progress?.problems ?? []);
  const projects = imp.targets.map((t, i) => t.existingKey || imp.config.projects[i]?.key).filter(Boolean) as string[];
  const resumable = imp.status === "failed" || imp.status === "cancelled" || imp.stalled;
  return (
    <div className="flex flex-col gap-4">
      <Card
        title={
          <span className="flex items-center gap-2">
            {running ? `Importing… ${overallPercent(progress)}%` : "Import"}
            <Badge tone={imp.stalled ? "warning" : STATUS_TONE[imp.status]}>{imp.stalled ? "Interrupted" : STATUS_LABEL[imp.status]}</Badge>
          </span>
        }
        actions={
          running ? (
            <Button size="sm" variant="ghost" icon={<Square className="size-3.5" />} loading={actions.cancel.isPending} onClick={() => actions.cancel.mutate()}>
              Stop
            </Button>
          ) : (
            <span className="text-xs text-muted">
              {imp.finishedAt ? (
                <>
                  Finished <When iso={imp.finishedAt} />
                </>
              ) : imp.startedAt ? (
                <>
                  Started <When iso={imp.startedAt} />
                </>
              ) : null}
            </span>
          )
        }
      >
        <div className="flex flex-col gap-4 p-4">
          {running && <ProgressBar value={overallPercent(progress)} label="Overall progress" />}
          {progress && <Phases progress={progress} failed={imp.status === "failed" || imp.stalled} />}
          {imp.error && <ErrorBox error={new Error(imp.error)} />}
          {imp.stalled && <Callout tone="warning" title="The import stopped checkpointing">The worker running it may have restarted. Resume it: it continues from the last checkpoint.</Callout>}
          {resumable && (
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="primary" icon={<RotateCcw className="size-4" />} loading={actions.run.isPending} disabled={!imp.hasCredentials} onClick={() => actions.run.mutate()}>
                Resume import
              </Button>
              <span className="text-xs text-muted">Continues from the last checkpoint; nothing is duplicated.</span>
            </div>
          )}
          {actions.run.error && <ErrorBox error={actions.run.error} />}
        </div>
      </Card>

      {rows.length > 0 && (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
          {rows.map((r) => (
            <Stat key={r.key} label={r.label} value={r.value} />
          ))}
        </div>
      )}

      {progress && (progress.problems.length > 0 || !running) && (
        <Card
          title="Report"
          actions={
            <span className="flex gap-1.5">
              {counts.error > 0 && <Badge tone="danger">{plural(counts.error, "error")}</Badge>}
              {counts.warning > 0 && <Badge tone="warning">{plural(counts.warning, "warning")}</Badge>}
              {counts.info > 0 && <Badge>{plural(counts.info, "note")}</Badge>}
            </span>
          }
        >
          <ProblemList problems={progress.problems} empty={running ? "No problems so far." : "Everything was imported without problems."} />
        </Card>
      )}

      {imp.status === "succeeded" && (
        <Card title="Next steps">
          <div className="flex flex-col gap-3 p-4 text-sm">
            <p className="flex flex-wrap items-center gap-2">
              Open the imported projects:
              {projects.map((k) => (
                <Link key={k} to="/projects/$projectKey/board" params={{ projectKey: k }} className="rounded bg-surface-2 px-2 py-0.5 font-mono text-[13px] text-accent hover:underline">
                  {k}
                </Link>
              ))}
            </p>
            <p className="text-muted">
              Running the import again later brings over what changed in Jira (unchanged issues are skipped). Placeholders become real accounts when their people sign in with
              their email.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button icon={<Play className="size-4" />} loading={actions.run.isPending} disabled={!imp.hasCredentials} onClick={() => actions.run.mutate()}>
                Import changes again
              </Button>
              {imp.hasCredentials && (
                <Button
                  variant="ghost"
                  icon={<KeyRound className="size-4" />}
                  loading={actions.deleteCredentials.isPending}
                  onClick={async () => {
                    if (
                      await confirm({
                        title: "Delete the Jira credentials?",
                        body: "The stored token is erased. Imported data stays; to import changes again later, enter a token again.",
                        confirmLabel: "Delete credentials",
                        danger: true,
                      })
                    )
                      actions.deleteCredentials.mutate();
                  }}
                >
                  Delete Jira credentials
                </Button>
              )}
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}
