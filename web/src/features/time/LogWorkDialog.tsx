import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import type { IssueDetail } from "@/api/types";
import { Button, Field, Input, Modal, Textarea, useToast } from "@/components/ui";
import { cn } from "@/lib/cn";
import { formatDuration, parseDuration } from "@/lib/duration";
import { useSaveWorklog, useTimeUnits, type Worklog, type WorklogInput, type WorklogPatch } from "./api";
import { autoRemaining, fromLocalInput, toLocalInput } from "./model";

type Adjust = "auto" | "set" | "leave";

/**
 * LogWorkDialog logs work on an issue, or changes a worklog (`worklog`): time spent, when the
 * work was done, a comment, and what happens to the remaining estimate (adjust automatically,
 * set it, or leave it).
 */
export function LogWorkDialog({ issue, worklog, open, onClose }: { issue: IssueDetail; worklog?: Worklog; open: boolean; onClose: () => void }) {
  return open ? <LogWorkForm key={worklog?.id ?? "new"} issue={issue} worklog={worklog} onClose={onClose} /> : null;
}

function LogWorkForm({ issue, worklog, onClose }: { issue: IssueDetail; worklog?: Worklog; onClose: () => void }) {
  const units = useTimeUnits();
  const toast = useToast();
  const save = useSaveWorklog(issue.key);
  const fmt = (s: number | undefined) => formatDuration(s, units);
  const [spent, setSpent] = useState(worklog ? fmt(worklog.timeSpentSeconds) : "");
  const [started, setStarted] = useState(toLocalInput(worklog ? new Date(worklog.startedAt) : new Date()));
  const [comment, setComment] = useState(worklog?.comment ?? "");
  const [adjust, setAdjust] = useState<Adjust>("auto");
  const [remaining, setRemaining] = useState("");
  const [touched, setTouched] = useState(false);
  const [serverErrors, setServerErrors] = useState<Record<string, string>>({});

  const parsed = spent.trim() ? parseDuration(spent, units) : undefined;
  const spentError = parsed?.error ?? (parsed && parsed.seconds < 60 ? "Log at least a minute" : undefined);
  const remainingParsed = adjust === "set" ? parseDuration(remaining, units) : undefined;
  const startedISO = fromLocalInput(started);
  const current = issue.timeTracking?.remainingSeconds;
  const delta = (parsed?.seconds ?? 0) - (worklog?.timeSpentSeconds ?? 0);
  const preview = autoRemaining(current, delta);
  const canSubmit = !!parsed?.seconds && !spentError && !!startedISO && (adjust !== "set" || !!remainingParsed?.seconds || remainingParsed?.seconds === 0);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!canSubmit || !parsed?.seconds || !startedISO) return;
    let body: WorklogInput | WorklogPatch;
    if (worklog) {
      const patch: WorklogPatch = {};
      if (parsed.seconds !== worklog.timeSpentSeconds) patch.timeSpentSeconds = parsed.seconds;
      if (new Date(startedISO).getTime() !== new Date(worklog.startedAt).getTime()) patch.startedAt = startedISO;
      if (comment.trim() !== worklog.comment) patch.comment = comment.trim();
      body = patch;
    } else {
      body = { timeSpentSeconds: parsed.seconds, startedAt: startedISO, comment: comment.trim() || undefined };
    }
    if (adjust === "leave") body.adjustEstimate = "leave";
    if (adjust === "set" && remainingParsed?.seconds != null) body.remainingEstimateSeconds = remainingParsed.seconds;
    save.mutate(
      { id: worklog?.id, body },
      {
        onSuccess: () => {
          toast.success(worklog ? "Work log updated" : `Logged ${fmt(parsed.seconds)} on ${issue.key}`);
          onClose();
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 400) setServerErrors(err.fieldErrors());
          toast.error(err);
        },
      },
    );
  };

  const options: { value: Adjust; label: string; hint?: string }[] = [
    {
      value: "auto",
      label: "Adjust automatically",
      hint: current == null ? "No remaining estimate to adjust" : parsed?.seconds ? `${fmt(current)} → ${fmt(preview)}` : `Currently ${fmt(current)}`,
    },
    { value: "set", label: "Set to" },
    { value: "leave", label: "Leave unchanged", hint: current != null ? `Stays ${fmt(current)}` : undefined },
  ];

  return (
    <Modal
      open
      onClose={onClose}
      title={worklog ? "Edit work log" : `Log work on ${issue.key}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" form="log-work-form" loading={save.isPending} disabled={touched && !canSubmit}>
            {worklog ? "Save" : "Log work"}
          </Button>
        </>
      }
    >
      <form id="log-work-form" onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Time spent"
            required
            error={(touched || spent.trim()) && spentError ? spentError : touched && !parsed ? "Enter the time spent" : serverErrors.timeSpentSeconds}
            hint={parsed?.seconds ? `= ${fmt(parsed.seconds)}` : `e.g. 2h 30m, 1d (${units.hoursPerDay}h), 1w (${units.daysPerWeek}d)`}
          >
            {(id) => <Input id={id} autoFocus value={spent} onChange={(e) => setSpent(e.target.value)} placeholder="2h 30m" invalid={!!spentError && touched} autoComplete="off" />}
          </Field>
          <Field label="Date started" required error={touched && !startedISO ? "Pick when the work was done" : serverErrors.startedAt}>
            {(id) => <Input id={id} type="datetime-local" value={started} onChange={(e) => setStarted(e.target.value)} />}
          </Field>
        </div>
        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium text-ink">Remaining estimate</legend>
          <div className="flex flex-col gap-1.5">
            {options.map((o) => (
              <label key={o.value} className="flex min-h-8 cursor-pointer items-center gap-2 text-sm">
                <input type="radio" name="adjust" value={o.value} checked={adjust === o.value} onChange={() => setAdjust(o.value)} className="size-4 accent-accent" />
                <span>{o.label}</span>
                {o.value === "set" && (
                  <Input
                    aria-label="New remaining estimate"
                    className={cn("h-7.5 w-32", adjust !== "set" && "opacity-60")}
                    value={remaining}
                    placeholder={current != null ? fmt(current) : "4h"}
                    onFocus={() => setAdjust("set")}
                    onChange={(e) => setRemaining(e.target.value)}
                    invalid={adjust === "set" && touched && !!remainingParsed?.error}
                  />
                )}
                {o.hint && <span className="text-xs text-muted">{o.hint}</span>}
                {o.value === "set" && adjust === "set" && remaining.trim() && (
                  <span className={cn("text-xs", remainingParsed?.error ? "text-danger" : "text-muted")}>{remainingParsed?.error ? "Not a duration" : `= ${fmt(remainingParsed?.seconds)}`}</span>
                )}
              </label>
            ))}
          </div>
        </fieldset>
        <Field label="Work description" hint="Optional" error={serverErrors.comment}>
          {(id) => <Textarea id={id} rows={3} value={comment} onChange={(e) => setComment(e.target.value)} placeholder="What did you work on?" maxLength={10000} />}
        </Field>
      </form>
    </Modal>
  );
}
