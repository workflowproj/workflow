import { CheckCircle2, Circle, Loader2, Square } from "lucide-react";
import { Button, Card } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { useImportActions } from "./api";
import { busy, overallPercent, phasePercent, type JiraImport, type Progress } from "./model";
import { ProgressBar } from "./parts";

/** Phases lists a run's phases with their progress. */
export function Phases({ progress, failed }: { progress: Progress; failed?: boolean }) {
  return (
    <ol className="flex flex-col gap-3">
      {progress.phases.map((ph) => {
        const pct = phasePercent(ph);
        const Icon = ph.status === "done" ? CheckCircle2 : ph.status === "running" ? (failed ? Circle : Loader2) : Circle;
        return (
          <li key={ph.key} className="grid grid-cols-[1.25rem_1fr_auto] items-center gap-x-2 gap-y-1">
            <Icon
              aria-hidden
              className={cn("size-4", ph.status === "done" ? "text-success" : ph.status === "running" ? (failed ? "text-danger" : "animate-spin text-accent") : "text-faint")}
            />
            <span className={cn("text-sm", ph.status === "pending" && "text-muted")}>{ph.label}</span>
            <span className="text-xs text-muted tabular-nums">{ph.total > 0 ? `${ph.done} / ${ph.total}` : ph.status === "done" ? "done" : ""}</span>
            <span />
            <div className="col-span-2">
              <ProgressBar value={pct} label={ph.label} tone={ph.status === "done" ? "success" : failed && ph.status === "running" ? "danger" : "accent"} />
            </div>
          </li>
        );
      })}
    </ol>
  );
}

/** ScanProgress shows a running analysis. */
export function ScanProgress({ imp }: { imp: JiraImport }) {
  const actions = useImportActions(imp.id);
  return (
    <Card
      title={`Analysing… ${overallPercent(imp.progress)}%`}
      actions={
        <Button size="sm" variant="ghost" icon={<Square className="size-3.5" />} loading={actions.cancel.isPending} onClick={() => actions.cancel.mutate()}>
          Stop
        </Button>
      }
    >
      <div className="flex flex-col gap-4 p-4">
        <p className="text-sm text-muted">Reading the selected projects from Jira. Nothing is written here yet; you can leave this page and come back.</p>
        {imp.progress && <Phases progress={imp.progress} />}
        {imp.stalled && <ErrorBox error={new Error("The analysis stopped checkpointing (the worker may have restarted). Stop it and analyse again.")} />}
        {!busy(imp) && !imp.stalled && <p className="text-sm text-muted">Finishing…</p>}
      </div>
    </Card>
  );
}
