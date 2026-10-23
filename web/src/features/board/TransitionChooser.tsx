import { ArrowRight } from "lucide-react";
import type { Issue } from "@/api/types";
import { Button, Modal } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import type { AvailableTransition } from "./queries";

/** TransitionChooser asks which transition to run when a column holds several reachable statuses. */
export function TransitionChooser({
  open,
  issue,
  columnName,
  options,
  onPick,
  onCancel,
}: {
  open: boolean;
  issue: Issue | null;
  columnName: string;
  options: AvailableTransition[];
  onPick: (t: AvailableTransition) => void;
  onCancel: () => void;
}) {
  return (
    <Modal open={open} onClose={onCancel} title={issue ? `Move ${issue.key} to ${columnName}` : "Move issue"} size="sm" footer={<Button onClick={onCancel}>Cancel</Button>}>
      <p className="mb-3 text-sm text-muted">This column has more than one status. Choose how to move the issue:</p>
      <div className="flex flex-col gap-1.5">
        {options.map((t, i) => (
          <button
            key={t.id}
            type="button"
            autoFocus={i === 0}
            onClick={() => onPick(t)}
            className="flex items-center gap-3 rounded-md border border-line px-3 py-2.5 text-left text-sm hover:border-accent hover:bg-accent-soft/40 focus-visible:outline-2 focus-visible:outline-accent"
          >
            <span className="flex-1 font-medium">{t.name}</span>
            <ArrowRight className="size-3.5 text-faint" />
            <StatusBadge status={t.to} />
          </button>
        ))}
      </div>
    </Modal>
  );
}
