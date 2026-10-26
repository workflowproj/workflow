import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleDashed } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, Field, Input, Modal, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { sprintsQuery, type Sprint } from "@/features/board/queries";
import { addDays, daysBetween, sprintDatesProblem, todayISO } from "./dates";

function useInvalidateSprints(projectKey: string) {
  const qc = useQueryClient();
  return () => {
    for (const k of [["backlog"], ["board"], ["sprints", projectKey], ["search"]]) qc.invalidateQueries({ queryKey: k });
  };
}

const fieldErrors = (e: unknown) => (e instanceof ApiError ? e.fieldErrors() : {});

/** EditSprintDialog changes a sprint's name, goal and dates. */
export function EditSprintDialog({ open, onClose, sprint, projectKey }: { open: boolean; onClose: () => void; sprint: Sprint | null; projectKey: string }) {
  const invalidate = useInvalidateSprints(projectKey);
  const [name, setName] = useState("");
  const [goal, setGoal] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  useEffect(() => {
    if (!open || !sprint) return;
    setName(sprint.name);
    setGoal(sprint.goal);
    setStart(sprint.startDate ?? "");
    setEnd(sprint.endDate ?? "");
  }, [open, sprint]);
  const save = useMutation({
    mutationFn: () =>
      unwrap(api.PATCH("/sprints/{sprintId}", { params: { path: { sprintId: sprint!.id } }, body: { name: name.trim(), goal, startDate: start || null, endDate: end || null } })),
    onSuccess: () => {
      invalidate();
      onClose();
    },
  });
  const fe = fieldErrors(save.error);
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Edit sprint"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="edit-sprint" loading={save.isPending} disabled={!name.trim()}>
            Save
          </Button>
        </>
      }
    >
      <form
        id="edit-sprint"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name" required error={fe.name}>
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={80} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Start date" error={fe.startDate}>
            {(id) => <Input id={id} type="date" value={start} onChange={(e) => setStart(e.target.value)} />}
          </Field>
          <Field label="End date" error={fe.endDate}>
            {(id) => <Input id={id} type="date" value={end} min={start || undefined} onChange={(e) => setEnd(e.target.value)} />}
          </Field>
        </div>
        <Field label="Sprint goal" error={fe.goal}>
          {(id) => <Textarea id={id} rows={3} value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="What should this sprint achieve?" />}
        </Field>
        {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}

const DURATIONS = [
  { value: "1", label: "1 week" },
  { value: "2", label: "2 weeks" },
  { value: "3", label: "3 weeks" },
  { value: "4", label: "4 weeks" },
  { value: "custom", label: "Custom" },
];

/** StartSprintDialog starts a future sprint; only one sprint can be active (409 otherwise). */
export function StartSprintDialog({
  open,
  onClose,
  sprint,
  projectKey,
  issueCount,
  points,
}: {
  open: boolean;
  onClose: () => void;
  sprint: Sprint | null;
  projectKey: string;
  issueCount: number;
  points: number;
}) {
  const invalidate = useInvalidateSprints(projectKey);
  const toast = useToast();
  const [name, setName] = useState("");
  const [goal, setGoal] = useState("");
  const [duration, setDuration] = useState("2");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  useEffect(() => {
    if (!open || !sprint) return;
    const s = sprint.startDate ?? todayISO();
    const e = sprint.endDate && sprint.endDate > s ? sprint.endDate : addDays(s, 14);
    const weeks = daysBetween(s, e) / 7;
    setName(sprint.name);
    setGoal(sprint.goal);
    setStart(s);
    setEnd(e);
    setDuration(Number.isInteger(weeks) && weeks >= 1 && weeks <= 4 ? String(weeks) : "custom");
  }, [open, sprint]);

  const changeStart = (v: string) => {
    setStart(v);
    if (v && duration !== "custom") setEnd(addDays(v, Number(duration) * 7));
  };
  const changeDuration = (v: string) => {
    setDuration(v);
    if (v !== "custom" && start) setEnd(addDays(start, Number(v) * 7));
  };

  const run = useMutation({
    mutationFn: async () => {
      const sprintId = sprint!.id;
      if (name.trim() && name.trim() !== sprint!.name) await unwrap(api.PATCH("/sprints/{sprintId}", { params: { path: { sprintId } }, body: { name: name.trim() } }));
      return unwrap(api.POST("/sprints/{sprintId}/start", { params: { path: { sprintId } }, body: { startDate: start, endDate: end, goal } }));
    },
    onSuccess: (s) => {
      invalidate();
      toast.success(`${s.name} started`);
      onClose();
    },
  });
  const fe = fieldErrors(run.error);
  // Dates that can't start a sprint disable the button; the field says why.
  const problem = sprintDatesProblem(start, end);
  const dateError = (field: "startDate" | "endDate") => fe[field] ?? (problem?.field === field ? problem.message : undefined);
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Start sprint"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="start-sprint" loading={run.isPending} disabled={!!problem}>
            Start sprint
          </Button>
        </>
      }
    >
      <form
        id="start-sprint"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          run.mutate();
        }}
      >
        <p className="text-sm text-muted">
          <span className="font-medium text-ink">{issueCount}</span> issue{issueCount === 1 ? "" : "s"} and{" "}
          <span className="font-medium text-ink">{points}</span> story point{points === 1 ? "" : "s"} will be included in this sprint.
        </p>
        <Field label="Sprint name" required error={fe.name}>
          {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} maxLength={80} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="Duration">
            {(id) => (
              <Select id={id} value={duration} onChange={(e) => changeDuration(e.target.value)}>
                {DURATIONS.map((d) => (
                  <option key={d.value} value={d.value}>
                    {d.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Start date" required error={dateError("startDate")}>
            {(id) => (
              <Input id={id} type="date" value={start} invalid={!!dateError("startDate")} aria-invalid={!!dateError("startDate")} onChange={(e) => changeStart(e.target.value)} />
            )}
          </Field>
          <Field label="End date" required error={dateError("endDate")}>
            {(id) => (
              <Input
                id={id}
                type="date"
                value={end}
                invalid={!!dateError("endDate")}
                aria-invalid={!!dateError("endDate")}
                min={start ? addDays(start, 1) : undefined}
                onChange={(e) => {
                  setEnd(e.target.value);
                  setDuration("custom");
                }}
              />
            )}
          </Field>
        </div>
        <Field label="Sprint goal" error={fe.goal}>
          {(id) => <Textarea id={id} rows={3} value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="What should this sprint achieve?" />}
        </Field>
        {run.error && Object.keys(fe).length === 0 && <ErrorBox error={run.error} />}
      </form>
    </Modal>
  );
}

const countQuery = (jql: string, enabled: boolean) => ({
  queryKey: ["search", jql, "count"],
  queryFn: () => unwrap(api.GET("/search", { params: { query: { jql, limit: 1 } } })).then((r) => r.total),
  enabled,
});

/** CompleteSprintDialog closes the active sprint and sends unfinished issues to the backlog or a future sprint. */
export function CompleteSprintDialog({
  open,
  onClose,
  sprint,
  projectKey,
  onCompleted,
}: {
  open: boolean;
  onClose: () => void;
  sprint: Sprint | null;
  projectKey: string;
  onCompleted?: (s: Sprint) => void;
}) {
  const invalidate = useInvalidateSprints(projectKey);
  const toast = useToast();
  const id = sprint?.id ?? "";
  const done = useQuery(countQuery(`sprint = ${id} AND statusCategory = Done`, open && !!id));
  const openCount = useQuery(countQuery(`sprint = ${id} AND statusCategory != Done`, open && !!id));
  const { data: sprints = [] } = useQuery({ ...sprintsQuery(projectKey), enabled: open });
  const future = sprints.filter((s) => s.state === "future");
  const [target, setTarget] = useState("backlog");
  useEffect(() => {
    if (open) setTarget(future[0]?.id ?? "backlog");
    // Only when the dialog opens: later sprint list refreshes keep the user's choice.
  }, [open]);

  const run = useMutation({
    mutationFn: async () => {
      const remaining = openCount.data ?? 0;
      let moveTo: string | undefined = remaining === 0 || target === "backlog" ? undefined : target;
      if (moveTo === "new") moveTo = (await unwrap(api.POST("/projects/{projectKey}/sprints", { params: { path: { projectKey } }, body: {} }))).id;
      return unwrap(api.POST("/sprints/{sprintId}/complete", { params: { path: { sprintId: id } }, body: { moveOpenIssuesTo: moveTo } }));
    },
    onSuccess: (s) => {
      invalidate();
      toast.success(`${s.name} completed`);
      onClose();
      onCompleted?.(s);
    },
  });
  const loading = done.isLoading || openCount.isLoading;
  const remaining = openCount.data ?? 0;
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={sprint ? `Complete ${sprint.name}` : "Complete sprint"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={run.isPending} disabled={loading} onClick={() => run.mutate()}>
            Complete sprint
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-3">
          <div className="flex items-center gap-3 rounded-md border border-line px-3 py-2.5">
            <CircleCheck className="size-5 text-done" />
            <div>
              <p className="text-lg leading-tight font-semibold tabular-nums">{loading ? "–" : (done.data ?? 0)}</p>
              <p className="text-xs text-muted">Completed issues</p>
            </div>
          </div>
          <div className="flex items-center gap-3 rounded-md border border-line px-3 py-2.5">
            <CircleDashed className="size-5 text-progress" />
            <div>
              <p className="text-lg leading-tight font-semibold tabular-nums">{loading ? "–" : remaining}</p>
              <p className="text-xs text-muted">Open issues</p>
            </div>
          </div>
        </div>
        {remaining > 0 ? (
          <Field label="Move open issues to" hint="Issues in a done status stay with the completed sprint.">
            {(fid) => (
              <Select id={fid} value={target} onChange={(e) => setTarget(e.target.value)}>
                <option value="backlog">Backlog</option>
                {future.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
                <option value="new">New sprint</option>
              </Select>
            )}
          </Field>
        ) : (
          !loading && <p className="text-sm text-muted">Every issue in this sprint is done. Nice work!</p>
        )}
        {run.error && <ErrorBox error={run.error} />}
      </div>
    </Modal>
  );
}
