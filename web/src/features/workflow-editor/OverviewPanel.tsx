import { useId } from "react";
import { ArrowRight, MousePointerClick, Plus, Repeat } from "lucide-react";
import { Button, Textarea } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import { addStatus, type WorkflowDoc } from "./model";
import { PanelSection } from "./panel-bits";
import type { Selection, WorkflowEditor } from "./useWorkflowEditor";

export function OverviewPanel({ doc, apply, onSelect }: { doc: WorkflowDoc; apply?: WorkflowEditor["apply"]; onSelect: (s: Selection) => void }) {
  const descId = useId();
  return (
    <div>
      <PanelSection title="Workflow">
        <p className="text-sm font-semibold">{doc.name}</p>
        <p className="mt-0.5 text-xs text-muted">The name identifies the workflow (also for `wfm workflow apply`) and cannot change here.</p>
        <label htmlFor={descId} className="mt-3 mb-1 block text-[13px] font-medium">
          Description
        </label>
        <Textarea id={descId} rows={2} value={doc.description ?? ""} disabled={!apply} onChange={(e) => apply?.((d) => ({ ...d, description: e.target.value || undefined }), { key: "description" })} />
      </PanelSection>

      {apply && (
        <PanelSection title="Editing">
          <ul className="flex flex-col gap-1.5 text-[13px] text-muted">
            <li className="flex gap-2">
              <MousePointerClick className="mt-0.5 size-3.5 shrink-0" /> Select a status or a transition label to edit it here.
            </li>
            <li className="flex gap-2">
              <ArrowRight className="mt-0.5 size-3.5 shrink-0" /> Drag from the dot on the right of a status onto another status to add a transition; drop on empty space to create a new status.
            </li>
            <li className="flex gap-2">
              <Repeat className="mt-0.5 size-3.5 shrink-0" /> “Any” badges are global transitions, available from every status.
            </li>
          </ul>
          <p className="mt-2 text-xs text-faint">Delete removes the selection · Ctrl+Z / Ctrl+Shift+Z undo and redo · arrow keys move a focused status.</p>
        </PanelSection>
      )}

      <PanelSection
        title={`Statuses (${doc.statuses.length})`}
        action={
          apply && (
            <Button
              size="sm"
              variant="ghost"
              className="h-6 px-1.5 text-xs"
              icon={<Plus className="size-3.5" />}
              onClick={() => {
                const r = addStatus(doc);
                apply(() => r.doc, { select: { kind: "status", name: r.name } });
              }}
            >
              Add
            </Button>
          )
        }
      >
        <ul className="flex flex-wrap gap-1.5">
          {doc.statuses.map((s) => (
            <li key={s.name}>
              <button
                type="button"
                onClick={() => onSelect({ kind: "status", name: s.name })}
                className="rounded hover:opacity-80 focus-visible:outline-2 focus-visible:outline-accent"
                aria-label={`Edit status ${s.name}`}
              >
                <StatusBadge status={s} />
              </button>
            </li>
          ))}
        </ul>
      </PanelSection>

      <PanelSection title={`Transitions (${doc.transitions.length})`}>
        <ul className="flex flex-col gap-0.5">
          {doc.transitions.map((t, i) => (
            <li key={i}>
              <button type="button" onClick={() => onSelect({ kind: "transition", index: i })} className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left text-sm hover:bg-surface-2">
                <span className="truncate font-medium">{t.name || "Unnamed"}</span>
                <span className="ml-auto shrink-0 truncate text-xs text-muted">
                  {t.from ? t.from.join(", ") : "Any"} → {t.to}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </PanelSection>
    </div>
  );
}
