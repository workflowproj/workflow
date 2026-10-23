import { createContext, memo, useContext } from "react";
import { Handle, Position, useConnection, type Node, type NodeProps } from "@xyflow/react";
import { CircleAlert, Play, Repeat } from "lucide-react";
import { categoryLabel } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import type { StatusNodeData } from "./graph";
import type { Selection } from "./useWorkflowEditor";

/** CanvasContext lets nodes and edge labels select elements without prop drilling. */
export const CanvasContext = createContext<{ select: (s: Selection) => void; selectedTransition?: number }>({ select: () => {} });

const accent = {
  todo: { bar: "bg-todo", text: "text-todo", soft: "bg-todo-soft" },
  in_progress: { bar: "bg-progress", text: "text-progress", soft: "bg-progress-soft" },
  done: { bar: "bg-done", text: "text-done", soft: "bg-done-soft" },
};

export const StatusNode = memo(function StatusNode({ id, data, selected }: NodeProps<Node<StatusNodeData>>) {
  const { select, selectedTransition } = useContext(CanvasContext);
  const connection = useConnection();
  const isDropTarget = connection.inProgress && connection.fromNode.id !== id;
  const a = accent[data.category];
  const hasErrors = data.errors.length > 0;
  return (
    <div className="relative w-[168px] rounded-lg">
      <div
        title={hasErrors ? data.errors.join("\n") : undefined}
        className={cn(
          "relative overflow-hidden rounded-lg border bg-surface shadow-sm transition-shadow",
          selected ? "border-accent ring-2 ring-accent/40" : hasErrors ? "border-danger ring-2 ring-danger/30" : "border-line-strong hover:shadow-md",
          isDropTarget && "ring-2 ring-accent/30",
        )}
      >
        <span aria-hidden className={cn("absolute inset-y-0 left-0 w-1", a.bar)} />
        <div className="flex items-center gap-2 py-2.5 pr-2.5 pl-3.5">
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] leading-tight font-semibold text-ink">{data.name || <span className="text-faint italic">Unnamed</span>}</p>
            <p className={cn("mt-0.5 text-[11px] font-medium", a.text)}>{categoryLabel[data.category] ?? data.category}</p>
          </div>
          {hasErrors && <CircleAlert className="size-4 shrink-0 text-danger" aria-label="Has errors" />}
          {data.initial && (
            <span
              title="Initial status: new issues start here"
              className="inline-flex h-5 shrink-0 items-center gap-0.5 rounded bg-surface-2 px-1.5 text-[10px] font-semibold tracking-wide text-muted uppercase"
            >
              <Play className="size-2.5 fill-current" /> Start
            </span>
          )}
        </div>
        {data.globals.length > 0 && (
          <div className={cn("flex flex-wrap gap-1 border-t border-line px-2 py-1.5", a.soft)}>
            {data.globals.map((g) => (
              <button
                key={g.index}
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  select({ kind: "transition", index: g.index });
                }}
                title={`"${g.name}" can be run from any status`}
                className={cn(
                  "nodrag inline-flex max-w-full items-center gap-1 rounded border bg-surface px-1.5 py-0.5 text-[11px] font-medium text-ink hover:border-accent",
                  selectedTransition === g.index ? "border-accent text-accent" : "border-line",
                )}
              >
                <Repeat className="size-3 shrink-0" />
                <span className="truncate">Any · {g.name}</span>
              </button>
            ))}
          </div>
        )}
      </div>
      <Handle type="target" position={Position.Left} className="wf-target-handle" isConnectableStart={false} style={{ pointerEvents: isDropTarget ? "all" : "none" }} />
      {/* Always rendered: edges need a source handle, even on read-only canvases. */}
      <Handle
        type="source"
        position={Position.Right}
        isConnectable={!data.readOnly}
        className="wf-source-handle"
        style={data.readOnly ? { visibility: "hidden" } : undefined}
        title="Drag to another status to add a transition, or to empty space to add a status"
      />
    </div>
  );
});
