import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, Field, Input, Modal, Select, Textarea } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import type { Workflow } from "@/features/settings/queries";
import { cn } from "@/lib/cn";
import { blankWorkflow, fromDefinition, parseWorkflowYaml, workflowToYaml } from "./model";

type Source = "blank" | "copy" | "yaml";

/** CreateWorkflowDialog creates a project workflow (version 1) from a template, a copy or YAML. */
export function CreateWorkflowDialog({ open, onClose, projectKey, workflows, copyFrom }: { open: boolean; onClose: () => void; projectKey: string; workflows: Workflow[]; copyFrom?: Workflow }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [source, setSource] = useState<Source>(copyFrom ? "copy" : "blank");
  const [name, setName] = useState(copyFrom ? `${copyFrom.name} (copy)` : "");
  const [copyId, setCopyId] = useState(copyFrom?.id ?? workflows[0]?.id ?? "");
  const [yaml, setYaml] = useState("");
  const parsed = source === "yaml" && yaml.trim() ? parseWorkflowYaml(yaml) : undefined;

  const create = useMutation({
    mutationFn: async () => {
      let doc;
      if (source === "blank") doc = blankWorkflow(name.trim());
      else if (source === "copy") {
        const w = await unwrap(api.GET("/workflows/{workflowId}", { params: { path: { workflowId: copyId } } }));
        const p = parseWorkflowYaml(w.version.yaml);
        doc = { ...(p.ok ? p.doc : fromDefinition(w.version.definition)), name: name.trim() };
      }
      const body = doc ? workflowToYaml(doc) : yaml;
      return unwrap(api.POST("/workflows", { body: { projectKey, yaml: body } }));
    },
    onSuccess: (w) => {
      qc.invalidateQueries({ queryKey: ["workflows"] });
      onClose();
      navigate({ to: "/projects/$projectKey/settings/workflows/$workflowId", params: { projectKey, workflowId: w.id } });
    },
  });
  const err = create.error;
  const fe = err instanceof ApiError ? err.fieldErrors() : {};
  const nameError = fe.name ?? (err instanceof ApiError && err.code === "duplicate_workflow" ? err.message : undefined);
  const canSubmit = source === "yaml" ? !!parsed?.ok : name.trim() !== "" && (source !== "copy" || !!copyId);

  const options: { id: Source; title: string; body: string }[] = [
    { id: "blank", title: "Blank", body: "To Do → In Progress → Done with start, stop, done and reopen transitions." },
    { id: "copy", title: "Copy a workflow", body: "Start from one of this project's or the shared workflows." },
    { id: "yaml", title: "Paste YAML", body: "A workflow document, e.g. exported or kept in a repository." },
  ];

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New workflow"
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="create-workflow" loading={create.isPending} disabled={!canSubmit}>
            Create and edit
          </Button>
        </>
      }
    >
      <form
        id="create-workflow"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (canSubmit) create.mutate();
        }}
      >
        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium">Start from</legend>
          <div className="grid gap-2 sm:grid-cols-3">
            {options.map((o) => (
              <button
                key={o.id}
                type="button"
                aria-pressed={source === o.id}
                onClick={() => setSource(o.id)}
                className={cn("rounded-md border p-3 text-left transition-colors", source === o.id ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
              >
                <p className="text-sm font-medium">{o.title}</p>
                <p className="mt-1 text-xs text-muted">{o.body}</p>
              </button>
            ))}
          </div>
        </fieldset>
        {source !== "yaml" && (
          <Field label="Name" required error={nameError} hint="Names are unique within the project and identify the workflow for `wfm workflow apply`.">
            {(id) => <Input id={id} autoFocus value={name} maxLength={80} onChange={(e) => setName(e.target.value)} placeholder="Support requests" invalid={!!nameError} />}
          </Field>
        )}
        {source === "copy" && (
          <Field label="Workflow to copy">
            {(id) => (
              <Select id={id} value={copyId} onChange={(e) => setCopyId(e.target.value)}>
                {workflows.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name} (v{w.currentVersion}
                    {w.shared ? ", shared" : ""})
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}
        {source === "yaml" && (
          <Field
            label="Workflow YAML"
            error={parsed && !parsed.ok ? `${parsed.line ? `Line ${parsed.line}: ` : ""}${parsed.error}` : nameError}
            hint={
              parsed?.ok
                ? `Creates “${parsed.doc.name}” with ${parsed.doc.statuses.length} statuses and ${parsed.doc.transitions.length} transitions.`
                : "kind: Workflow, name, statuses, transitions and an optional layout."
            }
          >
            {(id) => (
              <Textarea
                id={id}
                rows={12}
                spellCheck={false}
                value={yaml}
                onChange={(e) => setYaml(e.target.value)}
                className="font-mono text-[12px]"
                placeholder={workflowToYaml(blankWorkflow("Support requests")).split("\nlayout:")[0]}
              />
            )}
          </Field>
        )}
        {err && !nameError && <ErrorBox error={err} />}
      </form>
    </Modal>
  );
}
