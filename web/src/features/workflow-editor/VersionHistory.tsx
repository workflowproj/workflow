import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { History, RotateCcw } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, EmptyState, PageSpinner } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { When } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { formatDateTime } from "@/lib/format";
import { WorkflowCanvas } from "./Canvas";
import { fromDefinition, parseWorkflowYaml, type WorkflowDoc } from "./model";
import { YamlEditor } from "./YamlEditor";

/**
 * VersionHistory lists published versions; one can be viewed read-only and loaded into the
 * editor as a draft (publishing it restores it as a new version).
 */
export function VersionHistory({
  workflowId,
  current,
  onRestore,
  canEdit,
}: {
  workflowId: string;
  current: number;
  onRestore: (yaml: string, doc: WorkflowDoc, version: number) => void;
  canEdit: boolean;
}) {
  const versions = useQuery({
    queryKey: ["workflow-versions", workflowId],
    queryFn: () => unwrap(api.GET("/workflows/{workflowId}/versions", { params: { path: { workflowId } } })),
  });
  const [picked, setPicked] = useState<number | null>(null);
  const selected = picked ?? versions.data?.[0]?.version ?? null;
  const version = useQuery({
    queryKey: ["workflow-version", workflowId, selected],
    queryFn: () => unwrap(api.GET("/workflows/{workflowId}/versions/{version}", { params: { path: { workflowId, version: selected! } } })),
    enabled: selected !== null,
    staleTime: Infinity,
  });
  const [view, setView] = useState<"diagram" | "yaml">("diagram");

  if (versions.isLoading) return <PageSpinner />;
  if (versions.error) return <ErrorBox error={versions.error} />;
  if (!versions.data?.length) return <EmptyState icon={<History className="size-8" />} title="No versions yet" />;

  const v = version.data;
  const parsed = v ? parseWorkflowYaml(v.yaml) : undefined;
  const doc = v ? (parsed?.ok ? parsed.doc : fromDefinition(v.definition)) : undefined;

  return (
    <div className="flex min-h-[520px] flex-col gap-4 lg:flex-row">
      <ol aria-label="Versions" className="flex shrink-0 flex-col gap-1 lg:w-72">
        {versions.data.map((s) => (
          <li key={s.version}>
            <button
              type="button"
              onClick={() => setPicked(s.version)}
              aria-current={selected === s.version}
              className={cn(
                "flex w-full items-start gap-3 rounded-md border px-3 py-2.5 text-left",
                selected === s.version ? "border-accent bg-accent-soft" : "border-line bg-surface hover:border-line-strong",
              )}
            >
              <UserAvatar user={s.publishedBy} size={24} />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-2">
                  <span className="text-sm font-semibold">Version {s.version}</span>
                  {s.version === current && <Badge tone="success">Current</Badge>}
                </span>
                <span className="block truncate text-xs text-muted" title={formatDateTime(s.publishedAt)}>
                  {s.publishedBy?.displayName ?? "System"} · <When iso={s.publishedAt} />
                </span>
                {s.issueCount !== undefined && s.issueCount > 0 && (
                  <span className="mt-0.5 block text-xs text-muted">
                    {s.issueCount} issue{s.issueCount === 1 ? "" : "s"} still on this version
                  </span>
                )}
              </span>
            </button>
          </li>
        ))}
      </ol>
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-surface">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line px-3 py-2">
          <div role="tablist" aria-label="Version view" className="flex gap-1">
            {(["diagram", "yaml"] as const).map((k) => (
              <button
                key={k}
                role="tab"
                type="button"
                aria-selected={view === k}
                onClick={() => setView(k)}
                className={cn("rounded px-2.5 py-1 text-[13px]", view === k ? "bg-surface-2 font-medium text-ink" : "text-muted hover:text-ink")}
              >
                {k === "diagram" ? "Diagram" : "YAML"}
              </button>
            ))}
          </div>
          {canEdit && v && doc && selected !== current && (
            <Button size="sm" icon={<RotateCcw className="size-3.5" />} onClick={() => onRestore(v.yaml, doc, v.version)}>
              Load into editor
            </Button>
          )}
        </div>
        {version.isLoading || !doc || !v ? (
          <PageSpinner />
        ) : view === "diagram" ? (
          <WorkflowCanvas key={selected} doc={doc} selection={null} onSelect={() => {}} className="h-[480px] min-h-0 flex-1" />
        ) : (
          <YamlEditor value={v.yaml} readOnly className="m-3 h-[456px] flex-1" label={`YAML of version ${v.version}`} />
        )}
      </div>
    </div>
  );
}
