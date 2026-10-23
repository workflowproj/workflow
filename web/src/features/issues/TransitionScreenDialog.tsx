import { useEffect, useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, CircleAlert } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import type { IssueDetail, User } from "@/api/types";
import { Button, Modal, useToast } from "@/components/ui";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { StatusBadge } from "@/components/issue-bits";
import { afterTransition } from "./issue-api";
import { isEmptyValue, ScreenFieldControl, type ScreenField } from "./field-values";
import { TransitionProblem } from "./TransitionProblem";

type Values = Record<string, unknown>;

/**
 * TransitionScreenDialog collects a transition's screen fields (and an optional comment) and
 * runs it: POST /issues/{key}/transitions { transitionId, fields, comment }. Used by the issue
 * view's status menu and by boards when a drop answers 422 `screen_required` (the transition id
 * is in problem.errors[0].message; load the issue's available transitions to get its screen).
 */
export function TransitionScreenDialog({
  open,
  onClose,
  issueKey,
  transition,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  issueKey: string;
  transition: Schemas["AvailableTransition"] | null;
  onDone?: (issue: Schemas["Issue"]) => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const cached = qc.getQueryData<IssueDetail>(["issue", issueKey]);
  const projectKey = cached?.projectKey ?? issueKey.replace(/-\d+$/, "");
  const screen = transition?.screen ?? [];
  const commentOnScreen = screen.find((f) => f.field === "comment");
  const fields = screen.filter((f) => f.field !== "comment");

  const [values, setValues] = useState<Values>({});
  const [comment, setComment] = useState("");
  const [missing, setMissing] = useState<Record<string, string>>({});

  const run = useMutation({
    mutationFn: () => {
      const body: Schemas["TransitionRequest"] = { transitionId: transition!.id, fields: serialize(fields, values) };
      if (comment.trim()) body.comment = comment.trim();
      return unwrap(api.POST("/issues/{issueKey}/transitions", { params: { path: { issueKey } }, body }));
    },
    onSuccess: (issue) => {
      afterTransition(qc, issueKey, issue);
      toast.success(`${issue.key} moved to ${issue.status.name}`);
      onDone?.(issue);
      onClose();
    },
  });

  // Start from sensible defaults each time the dialog opens for a transition.
  useEffect(() => {
    if (!open || !transition) return;
    const v: Values = {};
    for (const f of transition.screen) {
      if (f.field === "resolution" && f.options?.includes("Done")) v.resolution = "Done";
      if (f.field === "assignee" && cached?.assignee) v.assignee = cached.assignee;
      if (f.field === "priority" && cached?.priority) v.priority = cached.priority;
      if (f.field === "labels" && cached?.labels) v.labels = cached.labels;
      if (f.field === "storyPoints" && cached?.storyPoints != null) v.storyPoints = String(cached.storyPoints);
      if (f.field === "dueDate" && cached?.dueDate) v.dueDate = cached.dueDate;
      if (f.field === "fixVersions" && cached?.releases) v.fixVersions = cached.releases.map((r) => r.id);
      if (cached?.fields && f.field in cached.fields) v[f.field] = cached.fields[f.field];
    }
    setValues(v);
    setComment("");
    setMissing({});
    run.reset();
    // Reset only when (re)opened, not when the cached issue refreshes underneath.
  }, [open, transition?.id]);

  const submit = () => {
    const errs: Record<string, string> = {};
    for (const f of fields) if (f.required && isEmptyValue(values[f.field])) errs[f.field] = `${f.name} is required`;
    if (commentOnScreen?.required && !comment.trim()) errs.comment = "A comment is required";
    setMissing(errs);
    if (Object.keys(errs).length === 0) run.mutate();
  };

  const serverErrors = run.error instanceof ApiError ? run.error.fieldErrors() : {};
  const errorFor = (field: string) => missing[field] ?? serverErrors[field] ?? serverErrors[`fields.${field}`];
  const known = new Set([...screen.map((f) => f.field), ...screen.map((f) => `fields.${f.field}`)]);
  const general = run.error && !(run.error instanceof ApiError && run.error.problem.errors?.length && run.error.problem.errors.every((e) => e.field && known.has(e.field)));

  return (
    <Modal
      open={open && !!transition}
      onClose={onClose}
      size="md"
      title={transition?.name ?? "Transition"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="transition-screen" loading={run.isPending}>
            {transition?.name ?? "Save"}
          </Button>
        </>
      }
    >
      {transition && (
        <form
          id="transition-screen"
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <p className="flex items-center gap-2 text-sm text-muted">
            <span className="font-mono text-xs">{issueKey}</span>
            {cached && <StatusBadge status={cached.status} />}
            <ArrowRight className="size-3.5 text-faint" />
            <StatusBadge status={transition.to} />
          </p>
          {general && <TransitionProblem error={run.error} />}
          {fields.map((f) => (
            <ScreenRow key={f.field} field={f} error={errorFor(f.field)}>
              {(id) => (
                <ScreenFieldControl
                  id={id}
                  field={f}
                  projectKey={projectKey}
                  value={values[f.field]}
                  invalid={!!errorFor(f.field)}
                  onChange={(v) => {
                    setValues((cur) => ({ ...cur, [f.field]: v }));
                    setMissing(({ [f.field]: _, ...rest }) => rest);
                  }}
                />
              )}
            </ScreenRow>
          ))}
          <ScreenRow field={commentOnScreen ?? { field: "comment", name: "Comment", type: "markdown", required: false }} error={errorFor("comment")} optional={!commentOnScreen}>
            {() => (
              <MarkdownEditor
                value={comment}
                onChange={(v) => {
                  setComment(v);
                  setMissing(({ comment: _, ...rest }) => rest);
                }}
                placeholder={commentOnScreen?.required ? "Explain the change…" : "Add a comment (optional)…"}
                minHeight={72}
                onSubmit={submit}
                className={errorFor("comment") ? "border-danger" : undefined}
              />
            )}
          </ScreenRow>
        </form>
      )}
    </Modal>
  );
}

function ScreenRow({ field, error, optional, children }: { field: ScreenField; error?: string; optional?: boolean; children: (id: string) => React.ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-[13px] font-medium">
        {field.name}
        {field.required && <span className="ml-0.5 text-danger">*</span>}
        {optional && <span className="ml-1.5 font-normal text-faint">optional</span>}
      </label>
      {children(id)}
      {error && (
        <p className="flex items-center gap-1 text-xs text-danger">
          <CircleAlert className="size-3.5" />
          {error}
        </p>
      )}
    </div>
  );
}

/** serialize turns form values into the API's screen values, leaving out empty optional ones. */
function serialize(fields: ScreenField[], values: Values): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    const v = values[f.field];
    if (f.type === "checkbox") {
      if (v !== undefined) out[f.field] = v === true;
      continue;
    }
    if (isEmptyValue(v)) continue;
    if (f.type === "user") out[f.field] = (v as User).id;
    else if (f.type === "number") out[f.field] = Number(v);
    else out[f.field] = v;
  }
  return out;
}
