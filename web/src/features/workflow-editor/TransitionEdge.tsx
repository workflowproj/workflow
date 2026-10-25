import { memo, useContext } from "react";
import { BaseEdge, EdgeLabelRenderer, useInternalNode, type Edge, type EdgeProps, type InternalNode, type Node } from "@xyflow/react";
import { ClipboardList, ShieldCheck, ListChecks, Zap } from "lucide-react";
import { cn } from "@/lib/cn";
import { edgePath, type TransitionEdgeData } from "./graph";
import { NODE_HEIGHT, NODE_WIDTH } from "./model";
import { CanvasContext } from "./StatusNode";

function rect(n: InternalNode<Node>) {
  return {
    x: n.internals.positionAbsolute.x,
    y: n.internals.positionAbsolute.y,
    width: n.measured.width ?? NODE_WIDTH,
    height: n.measured.height ?? NODE_HEIGHT,
  };
}

/** TransitionEdge is a "floating" curved edge between two status cards with its name as a label. */
export const TransitionEdge = memo(function TransitionEdge({ id, source, target, data, selected }: EdgeProps<Edge<TransitionEdgeData>>) {
  const { select, selectedTransition } = useContext(CanvasContext);
  const s = useInternalNode(source);
  const t = useInternalNode(target);
  if (!s || !t || !data) return null;
  const { path, labelX, labelY } = edgePath(rect(s), rect(t), data.offset);
  const active = selected || selectedTransition === data.index;
  const marker = active ? "url(#wf-arrow-active)" : data.error ? "url(#wf-arrow-error)" : "url(#wf-arrow)";
  const details = [
    data.hasCondition && { icon: ShieldCheck, label: "Has a condition" },
    data.validators > 0 && { icon: ListChecks, label: `${data.validators} validator${data.validators > 1 ? "s" : ""}` },
    data.screen > 0 && { icon: ClipboardList, label: `Screen with ${data.screen} field${data.screen > 1 ? "s" : ""}` },
    data.postFunctions > 0 && { icon: Zap, label: `${data.postFunctions} post-function${data.postFunctions > 1 ? "s" : ""}` },
  ].filter(Boolean) as { icon: typeof Zap; label: string }[];
  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={marker} interactionWidth={18} style={{ strokeWidth: active ? 2.25 : 1.5 }} />
      <EdgeLabelRenderer>
        <button
          type="button"
          tabIndex={-1}
          onClick={() => select({ kind: "transition", index: data.index, from: data.from })}
          style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, pointerEvents: "all" }}
          className={cn(
            "nodrag nopan absolute inline-flex max-w-44 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium whitespace-nowrap shadow-sm transition-colors",
            active ? "z-10 border-accent bg-accent text-accent-ink" : data.error ? "border-danger/50 bg-danger-soft text-danger" : "border-line bg-surface text-ink hover:border-line-strong",
          )}
          title={[data.name, ...details.map((d) => d.label)].join(" · ")}
        >
          <span className="truncate">{data.name || "Unnamed"}</span>
          {details.map(({ icon: Icon, label }) => (
            <Icon key={label} className="size-3 shrink-0 opacity-80" aria-label={label} />
          ))}
        </button>
      </EdgeLabelRenderer>
    </>
  );
});

/** EdgeMarkers defines the arrowheads used by TransitionEdge; render once per page with a canvas. */
export function EdgeMarkers() {
  const arrow = (id: string, color: string) => (
    <marker id={id} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
      <path d="M 0 0 L 10 5 L 0 10 z" style={{ fill: color }} />
    </marker>
  );
  return (
    <svg aria-hidden className="pointer-events-none absolute size-0 overflow-hidden">
      <defs>
        {arrow("wf-arrow", "var(--color-faint)")}
        {arrow("wf-arrow-active", "var(--color-accent)")}
        {arrow("wf-arrow-error", "var(--color-danger)")}
      </defs>
    </svg>
  );
}
