import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  BackgroundVariant,
  ConnectionLineType,
  Controls,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type FinalConnectionState,
  type Node,
  type NodeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./editor.css";
import { LayoutGrid, Plus } from "lucide-react";
import { Button } from "@/components/ui";
import { buildGraph, parseEdgeId, routeAroundNodes, type Rect, type StatusNodeData, type TransitionEdgeData } from "./graph";
import { addStatus, addTransition, NODE_HEIGHT, NODE_WIDTH, setPosition, tidyLayout, type WorkflowDoc } from "./model";
import { CanvasContext, StatusNode } from "./StatusNode";
import { EdgeMarkers, TransitionEdge } from "./TransitionEdge";
import type { Selection, WorkflowEditor } from "./useWorkflowEditor";

const nodeTypes = { status: StatusNode };
const edgeTypes = { transition: TransitionEdge };

type Props = {
  doc: WorkflowDoc;
  selection: Selection;
  onSelect: (s: Selection) => void;
  /** Absent for read-only canvases (e.g. viewing an old version). */
  apply?: WorkflowEditor["apply"];
  statusErrors?: Map<number, string[]>;
  transitionErrors?: Set<number>;
  onDeleteSelection?: () => void;
  className?: string;
};

export function WorkflowCanvas(props: Props) {
  return (
    <ReactFlowProvider>
      <CanvasInner {...props} />
    </ReactFlowProvider>
  );
}

function CanvasInner({ doc, selection, onSelect, apply, statusErrors, transitionErrors, onDeleteSelection, className }: Props) {
  const readOnly = !apply;
  const wrapper = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition, getViewport, fitView } = useReactFlow();
  const graph = useMemo(() => buildGraph(doc, { statusErrors, transitionErrors, readOnly }), [doc, statusErrors, transitionErrors, readOnly]);

  // Nodes live in state so React Flow can keep their measured size and drag position; the
  // document stays the source of truth and is re-applied whenever it changes.
  const [nodes, setNodes] = useState<Node<StatusNodeData>[]>([]);
  useEffect(() => {
    setNodes((prev) => {
      const byId = new Map(prev.map((n) => [n.id, n]));
      return graph.nodes.map((g) => {
        const p = byId.get(g.id);
        return {
          ...g,
          type: "status",
          measured: p?.measured,
          position: p?.dragging ? p.position : g.position,
          dragging: p?.dragging,
          selected: selection?.kind === "status" && selection.name === g.id,
          ariaLabel: `Status ${g.data.name}`,
        };
      });
    });
  }, [graph, selection]);

  const selectedTransition = selection?.kind === "transition" ? selection.index : undefined;
  const edges = useMemo<Edge<TransitionEdgeData>[]>(() => {
    // Route with the live node rectangles so edges bend around statuses while dragging too.
    const rects = new Map<string, Rect>(nodes.map((n) => [n.id, { x: n.position.x, y: n.position.y, width: n.measured?.width ?? NODE_WIDTH, height: n.measured?.height ?? NODE_HEIGHT }]));
    const routed = graph.edges.map((e) => ({ ...e, data: { ...e.data } }));
    routeAroundNodes(routed, rects);
    return routed.map((e) => ({
      ...e,
      type: "transition",
      selected: selectedTransition === e.data.index && (selection?.kind !== "transition" || !selection.from || selection.from === e.source),
      className: selectedTransition === e.data.index ? "wf-active" : e.data.error ? "wf-error" : undefined,
      ariaLabel: `Transition ${e.data.name} from ${e.source} to ${e.target}`,
    }));
  }, [graph, nodes, selectedTransition, selection]);

  const persist = useCallback((id: string, position: { x: number; y: number }) => apply?.((d) => setPosition(d, id, position), { key: `move:${id}` }), [apply]);

  const onNodesChange = useCallback(
    (changes: NodeChange<Node<StatusNodeData>>[]) => {
      setNodes((ns) =>
        applyNodeChanges(
          changes.filter((c) => c.type !== "remove" && c.type !== "select"),
          ns,
        ),
      );
      for (const c of changes) {
        if (c.type === "select" && c.selected) onSelect({ kind: "status", name: c.id });
        // Keyboard moves arrive without `dragging`; mouse drags are saved on drag stop.
        if (c.type === "position" && c.position && c.dragging === undefined) persist(c.id, c.position);
      }
    },
    [onSelect, persist],
  );

  const onEdgesChange = useCallback(
    (changes: EdgeChange<Edge<TransitionEdgeData>>[]) => {
      for (const c of changes) {
        if (c.type === "select" && c.selected) {
          const ref = parseEdgeId(c.id);
          if (ref) onSelect({ kind: "transition", index: ref.index, from: ref.from });
        }
      }
    },
    [onSelect],
  );

  const onConnect = useCallback(
    (c: Connection) => {
      if (!apply || !c.source || !c.target || c.source === c.target) return;
      const r = addTransition(doc, c.source, c.target);
      apply(() => r.doc, { select: { kind: "transition", index: r.index, from: c.source } });
    },
    [apply, doc],
  );

  // Dropping a new connection on empty canvas creates a status there, linked from the source.
  const onConnectEnd = useCallback(
    (event: MouseEvent | TouchEvent, state: FinalConnectionState) => {
      if (!apply || state.isValid || !state.fromNode || state.toNode) return;
      const point = "changedTouches" in event ? event.changedTouches[0] : event;
      const pos = screenToFlowPosition({ x: point.clientX, y: point.clientY });
      const s = addStatus(doc, { position: { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 } });
      const t = addTransition(s.doc, state.fromNode.id, s.name);
      apply(() => t.doc, { select: { kind: "status", name: s.name } });
    },
    [apply, doc, screenToFlowPosition],
  );

  const addAtCenter = () => {
    if (!apply) return;
    const box = wrapper.current?.getBoundingClientRect();
    const { zoom } = getViewport();
    const pos = box ? screenToFlowPosition({ x: box.left + box.width / 2, y: box.top + box.height / 2 }) : { x: 0, y: 0 };
    const jitter = (doc.statuses.length % 4) * 24;
    const s = addStatus(doc, { position: { x: pos.x - NODE_WIDTH / 2 + jitter / zoom, y: pos.y - NODE_HEIGHT / 2 + jitter / zoom } });
    apply(() => s.doc, { select: { kind: "status", name: s.name } });
  };

  const tidy = () => {
    if (!apply) return;
    apply((d) => ({ ...d, layout: tidyLayout(d) }));
    requestAnimationFrame(() => fitView({ padding: 0.2, duration: 300 }));
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    const t = e.target as HTMLElement;
    if (/^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || t.isContentEditable) return;
    if ((e.key === "Delete" || e.key === "Backspace") && selection && onDeleteSelection) {
      e.preventDefault();
      onDeleteSelection();
    } else if (e.key === "Escape" && selection) {
      onSelect(null);
    }
  };

  return (
    <CanvasContext.Provider value={{ select: onSelect, selectedTransition }}>
      <div ref={wrapper} className={className} onKeyDown={onKeyDown}>
        <EdgeMarkers />
        <ReactFlow
          className="wf-canvas"
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeDragStop={(_, __, dragged) => dragged.forEach((n) => persist(n.id, n.position))}
          onNodeClick={(_, n) => onSelect({ kind: "status", name: n.id })}
          onEdgeClick={(_, e) => {
            const ref = parseEdgeId(e.id);
            if (ref) onSelect({ kind: "transition", index: ref.index, from: ref.from });
          }}
          onPaneClick={() => onSelect(null)}
          onConnect={onConnect}
          onConnectEnd={onConnectEnd}
          isValidConnection={(c) => c.source !== c.target}
          nodesDraggable={!readOnly}
          nodesConnectable={!readOnly}
          deleteKeyCode={null}
          multiSelectionKeyCode={null}
          selectionKeyCode={null}
          connectionLineType={ConnectionLineType.Straight}
          connectionRadius={36}
          fitView
          fitViewOptions={{ padding: 0.2, maxZoom: 1 }}
          minZoom={0.3}
          maxZoom={1.75}
          proOptions={{ hideAttribution: true }}
        >
          <Background variant={BackgroundVariant.Dots} gap={18} size={1} />
          <Controls showInteractive={false} position="bottom-left" />
          {!readOnly && (
            <Panel position="top-left" className="flex gap-1.5">
              <Button size="sm" icon={<Plus className="size-3.5" />} onClick={addAtCenter}>
                Add status
              </Button>
              <Button size="sm" icon={<LayoutGrid className="size-3.5" />} onClick={tidy} title="Arrange statuses left to right from the initial status">
                Tidy up
              </Button>
            </Panel>
          )}
        </ReactFlow>
      </div>
    </CanvasContext.Provider>
  );
}
