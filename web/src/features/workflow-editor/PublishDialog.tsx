import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Rocket } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Badge, Button, Modal, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { Callout } from "@/features/settings/common";
import { StatusMappingFields, suggestMapping, type RemovedStatus } from "@/features/settings/StatusMapping";
import { diffDocs, type WorkflowDoc } from "./model";

type Detail = Schemas["WorkflowDetail"];

/**
 * PublishDialog publishes the draft as a new version. When issues sit in statuses the draft
 * removes, it collects where they should go (old status -> new status) first; the API answers
 * 422 status_mapping_required otherwise, and any statuses it reports are added to the form.
 */
export function PublishDialog({
  open,
  onClose,
  workflow,
  yaml,
  doc,
  baseline,
  renames,
  removed,
  onPublished,
}: {
  open: boolean;
  onClose: () => void;
  workflow: Detail;
  yaml: string;
  doc: WorkflowDoc;
  baseline: WorkflowDoc;
  renames: Record<string, string>;
  removed: RemovedStatus[];
  onPublished: (d: Detail) => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const [extra, setExtra] = useState<RemovedStatus[]>([]);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const all = useMemo(() => {
    const seen = new Set(removed.map((r) => r.name.toLowerCase()));
    return [...removed, ...extra.filter((e) => !seen.has(e.name.toLowerCase()))];
  }, [removed, extra]);
  const targets = doc.statuses;
  const oldCategory = (n: string) => baseline.statuses.find((s) => s.name.toLowerCase() === n.toLowerCase())?.category;

  useEffect(() => {
    if (!open) return;
    setMapping((m) => suggestMapping(all, targets, oldCategory, m, renames));
  }, [open, all, targets, renames]);

  useEffect(() => {
    if (!open) setExtra([]);
  }, [open]);

  const publish = useMutation({
    mutationFn: () => {
      const statusMapping = Object.fromEntries(all.filter((r) => mapping[r.name]).map((r) => [r.name, mapping[r.name]]));
      return unwrap(api.POST("/workflows/{workflowId}/versions", { params: { path: { workflowId: workflow.id } }, body: { yaml, statusMapping } }));
    },
    onSuccess: (d) => {
      qc.setQueryData(["workflow", workflow.id], d);
      for (const k of ["workflows", "workflow-versions", "statuses", "issue-types", "board", "backlog", "search"]) qc.invalidateQueries({ queryKey: [k] });
      toast.success(d.pendingMigration ? `Published version ${d.currentVersion}. Issues are being migrated in the background.` : `Published version ${d.currentVersion}.`);
      onPublished(d);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "status_mapping_required") {
        setExtra((e.problem.errors ?? []).filter((f) => f.field).map((f) => ({ name: f.field!, message: f.message })));
      }
    },
  });

  const needsMapping = all.length > 0;
  const complete = all.every((r) => mapping[r.name]);
  const mappingError = publish.error instanceof ApiError && publish.error.code === "status_mapping_required";
  const diff = diffDocs(baseline, doc);
  const lines: [string, string[]][] = [
    ["Statuses added", diff.addedStatuses],
    ["Statuses removed", diff.removedStatuses],
    ["Statuses changed", diff.changedStatuses],
    ["Transitions added", diff.addedTransitions],
    ["Transitions removed", diff.removedTransitions],
    ["Transitions changed", diff.changedTransitions],
  ];
  const affected = all.reduce((n, r) => n + (r.issueCount ?? 0), 0);

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Publish version ${workflow.currentVersion + 1}`}
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" icon={<Rocket className="size-4" />} loading={publish.isPending} disabled={needsMapping && !complete} onClick={() => publish.mutate()}>
            {needsMapping ? "Publish and migrate issues" : "Publish"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div>
          <p className="mb-2 text-sm font-medium">Changes from version {workflow.currentVersion}</p>
          <dl className="grid gap-x-4 gap-y-1.5 text-sm sm:grid-cols-[10rem_1fr]">
            {lines
              .filter(([, v]) => v.length)
              .map(([label, v]) => (
                <div key={label} className="contents">
                  <dt className="text-muted">{label}</dt>
                  <dd className="flex flex-wrap gap-1">
                    {v.map((n) => (
                      <Badge key={n} className="!normal-case">
                        {n}
                      </Badge>
                    ))}
                  </dd>
                </div>
              ))}
            {(diff.layoutChanged || diff.descriptionChanged) && (
              <div className="contents">
                <dt className="text-muted">Other</dt>
                <dd>{[diff.descriptionChanged && "description", diff.layoutChanged && "diagram layout"].filter(Boolean).join(", ")}</dd>
              </div>
            )}
          </dl>
          {lines.every(([, v]) => v.length === 0) && !diff.layoutChanged && !diff.descriptionChanged && <p className="text-sm text-muted">No structural changes; a new version is still recorded.</p>}
        </div>

        {needsMapping ? (
          <div className="flex flex-col gap-2">
            <Callout tone={mappingError ? "danger" : "warning"} title={`${affected ? `${affected} issue${affected === 1 ? " is" : "s are"}` : "Issues are"} in statuses this version removes`}>
              Choose where they should go. After publishing, a background job moves them and pins them to the new version.
            </Callout>
            <StatusMappingFields removed={all} targets={targets} value={mapping} onChange={setMapping} oldCategory={oldCategory} />
          </div>
        ) : (
          <Callout title="What happens next">
            New issues use version {workflow.currentVersion + 1} immediately. Existing issues are moved to it by a background migration; their status is kept because every status they use still
            exists.
          </Callout>
        )}
        {publish.error && !mappingError && <ErrorBox error={publish.error} />}
      </div>
    </Modal>
  );
}
