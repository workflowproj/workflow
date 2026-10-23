import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Repeat, Trash2 } from "lucide-react";
import { statusesQuery } from "@/api/queries";
import { Button, Checkbox, Input } from "@/components/ui";
import { categoryLabel } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import { CATEGORIES, findStatus, transitionsOf, updateStatus, type Category, type WorkflowDoc } from "./model";
import { errorAt, FieldError, PanelSection, type PathError } from "./panel-bits";
import type { Selection, WorkflowEditor } from "./useWorkflowEditor";

const catStyle: Record<Category, string> = {
  todo: "data-[on=true]:border-todo data-[on=true]:bg-todo-soft data-[on=true]:text-todo",
  in_progress: "data-[on=true]:border-progress data-[on=true]:bg-progress-soft data-[on=true]:text-progress",
  done: "data-[on=true]:border-done data-[on=true]:bg-done-soft data-[on=true]:text-done",
};

export function StatusPanel({
  doc,
  name,
  apply,
  onSelect,
  onDelete,
  errors,
  published,
}: {
  doc: WorkflowDoc;
  name: string;
  apply?: WorkflowEditor["apply"];
  onSelect: (s: Selection) => void;
  onDelete: () => void;
  errors: PathError[];
  /** Whether the status exists in the published version (new ones also rename their transitions). */
  published: boolean;
}) {
  const index = doc.statuses.findIndex((s) => s.name === name);
  const status = doc.statuses[index];
  const { data: known = [] } = useQuery(statusesQuery);
  const listId = useId();
  const nameId = useId();
  // Rename on blur/enter so transitions follow the new name in one undo step.
  const [draft, setDraft] = useState(name);
  useEffect(() => setDraft(name), [name]);
  if (!status) return null;
  const readOnly = !apply;
  const prefix = `statuses[${index}]`;
  const { incoming, outgoing } = transitionsOf(doc, name);
  const globals = doc.transitions.map((t, i) => ({ t, i })).filter(({ t }) => !t.from);
  const existing = known.find((s) => s.name.toLowerCase() === draft.trim().toLowerCase());
  const conflict = existing && existing.category !== status.category && !doc.statuses.some((s, i) => i !== index && s.name === existing.name);

  const commitName = () => {
    const next = draft.trim();
    if (!apply || !next || next === name) return setDraft(name);
    if (findStatus(doc, next) && next.toLowerCase() !== name.toLowerCase()) return setDraft(name);
    const match = known.find((s) => s.name.toLowerCase() === next.toLowerCase());
    apply((d) => updateStatus(d, name, { name: next, ...(match ? { category: match.category as Category } : {}) }, { renameAutoNamed: !published }), {
      select: { kind: "status", name: next },
      rename: { from: name, to: next },
    });
  };
  const duplicate = draft.trim() && draft.trim().toLowerCase() !== name.toLowerCase() && !!findStatus(doc, draft.trim());

  return (
    <div>
      <PanelSection title="Status">
        <label htmlFor={nameId} className="mb-1 block text-[13px] font-medium">
          Name
        </label>
        <Input
          id={nameId}
          value={draft}
          list={listId}
          disabled={readOnly}
          invalid={!!duplicate || !!errorAt(errors, prefix, ".name")}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commitName}
          onKeyDown={(e) => {
            if (e.key === "Enter") commitName();
            if (e.key === "Escape") setDraft(name);
          }}
        />
        <datalist id={listId}>
          {known.map((s) => (
            <option key={s.id} value={s.name} />
          ))}
        </datalist>
        <FieldError message={duplicate ? "Another status already has this name" : errorAt(errors, prefix, ".name")} />
        <p className="mt-1 text-xs text-muted">
          Statuses are shared by name across workflows, boards and JQL. Renaming a published status replaces it: you choose where its issues go when you publish.
        </p>

        <p className="mt-3 mb-1 text-[13px] font-medium">Category</p>
        <div role="radiogroup" aria-label="Category" className="grid grid-cols-3 gap-1.5">
          {CATEGORIES.map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={status.category === c}
              data-on={status.category === c}
              disabled={readOnly}
              onClick={() => apply?.((d) => updateStatus(d, name, { category: c }))}
              className={cn("rounded-md border border-line px-2 py-1.5 text-xs font-medium text-muted hover:border-line-strong disabled:cursor-default", catStyle[c])}
            >
              {categoryLabel[c]}
            </button>
          ))}
        </div>
        <FieldError message={errorAt(errors, prefix, ".category")} />
        {conflict && (
          <p className="mt-1 text-xs text-warning">
            “{existing.name}” already exists as {categoryLabel[existing.category]}; a shared status cannot change category.
          </p>
        )}
        <p className="mt-1 text-xs text-muted">Done statuses resolve issues and count as completed in reports.</p>

        <Checkbox
          className="mt-3"
          label="Initial status (new issues start here)"
          checked={!!status.initial || (index === 0 && !doc.statuses.some((s) => s.initial))}
          disabled={readOnly}
          onChange={(e) => apply?.((d) => updateStatus(d, name, { initial: e.target.checked }))}
        />
      </PanelSection>

      <PanelSection title={`Transitions (${incoming.length + outgoing.length})`}>
        {incoming.length + outgoing.length + globals.length === 0 ? (
          <p className="text-sm text-muted">No transitions yet. Drag from the handle on the right of a status onto another to connect them.</p>
        ) : (
          <ul className="flex flex-col gap-0.5">
            {outgoing.map((i) => (
              <TransitionLink
                key={`o${i}`}
                icon={<ArrowRight className="size-3.5 text-faint" />}
                label={doc.transitions[i].name}
                detail={`to ${doc.transitions[i].to}`}
                onClick={() => onSelect({ kind: "transition", index: i, from: name })}
              />
            ))}
            {incoming.map((i) => (
              <TransitionLink
                key={`i${i}`}
                icon={doc.transitions[i].from ? <ArrowLeft className="size-3.5 text-faint" /> : <Repeat className="size-3.5 text-faint" />}
                label={doc.transitions[i].name}
                detail={doc.transitions[i].from ? `from ${doc.transitions[i].from!.join(", ")}` : "from any status"}
                onClick={() => onSelect({ kind: "transition", index: i })}
              />
            ))}
            {globals
              .filter(({ t }) => t.to.toLowerCase() !== name.toLowerCase())
              .map(({ t, i }) => (
                <TransitionLink
                  key={`g${i}`}
                  icon={<Repeat className="size-3.5 text-faint" />}
                  label={t.name}
                  detail={`to ${t.to} (from any status)`}
                  onClick={() => onSelect({ kind: "transition", index: i })}
                />
              ))}
          </ul>
        )}
      </PanelSection>

      {!readOnly && (
        <PanelSection title="Danger zone">
          <Button variant="secondary" size="sm" icon={<Trash2 className="size-3.5 text-danger" />} onClick={onDelete} disabled={doc.statuses.length <= 1}>
            Delete status
          </Button>
          <p className="mt-1.5 text-xs text-muted">Transitions into it are removed. Issues in this status are moved when you publish, to a status you choose.</p>
        </PanelSection>
      )}
    </div>
  );
}

function TransitionLink({ icon, label, detail, onClick }: { icon: React.ReactNode; label: string; detail: string; onClick: () => void }) {
  return (
    <li>
      <button type="button" onClick={onClick} className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left text-sm hover:bg-surface-2">
        {icon}
        <span className="truncate font-medium">{label}</span>
        <span className="ml-auto truncate text-xs text-muted">{detail}</span>
      </button>
    </li>
  );
}
