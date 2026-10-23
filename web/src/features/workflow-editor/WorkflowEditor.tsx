import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useBlocker } from "@tanstack/react-router";
import { ChevronRight, Download, Lock, Redo2, RefreshCw, Rocket, Undo2, Wand2 } from "lucide-react";
import type { Schemas } from "@/api/client";
import { useMe } from "@/api/queries";
import { Badge, Button, IconButton, Spinner, useConfirm } from "@/components/ui";
import { Callout, CopyButton } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { WorkflowCanvas } from "./Canvas";
import { deleteStatus, deleteTransition, fromDefinition, parseWorkflowYaml, removeEdge, transitionsOf, type WorkflowDoc } from "./model";
import { OverviewPanel } from "./OverviewPanel";
import { PublishDialog } from "./PublishDialog";
import { StatusPanel } from "./StatusPanel";
import { TransitionPanel } from "./TransitionPanel";
import { useWorkflowEditor } from "./useWorkflowEditor";
import { errorTargets, ProblemsList, useWorkflowValidation, ValidationBadge } from "./validation";
import { VersionHistory } from "./VersionHistory";
import { YamlEditor } from "./YamlEditor";

type Detail = Schemas["WorkflowDetail"];
type Tab = "diagram" | "yaml" | "history";

function initialDraft(w: Detail): { text: string; doc: WorkflowDoc } {
  const parsed = parseWorkflowYaml(w.version.yaml);
  return { text: w.version.yaml, doc: parsed.ok ? parsed.doc : fromDefinition(w.version.definition) };
}

export function WorkflowEditor({ workflow, projectKey }: { workflow: Detail; projectKey: string }) {
  const { data: me } = useMe();
  const confirm = useConfirm();
  const canEdit = !workflow.shared || me?.orgRole === "admin";
  const ed = useWorkflowEditor(initialDraft(workflow));
  const [baseVersion, setBaseVersion] = useState(workflow.currentVersion);
  const [tab, setTab] = useState<Tab>("diagram");
  const [publishing, setPublishing] = useState(false);
  const validation = useWorkflowValidation(ed.yaml, workflow.id, !ed.parseError);
  const { statusErrors, transitionErrors } = useMemo(() => errorTargets(validation.errors), [validation.errors]);
  const apply = canEdit && !ed.parseError ? ed.apply : undefined;
  const nameMismatch = ed.doc.name.trim() !== workflow.name;
  const errorCount = validation.errors.length + (nameMismatch ? 1 : 0);
  const valid = !ed.parseError && validation.upToDate && validation.result?.valid && !nameMismatch;
  const removed = validation.result?.removedStatuses ?? [];

  useBlocker({
    shouldBlockFn: async () => {
      if (!ed.dirty) return false;
      const leave = await confirm({
        title: "Discard unpublished changes?",
        body: "Your draft of this workflow has not been published and will be lost.",
        confirmLabel: "Discard and leave",
        danger: true,
      });
      return !leave;
    },
    enableBeforeUnload: () => ed.dirty,
  });

  // Undo / redo outside text fields (those keep their native undo).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (!(e.metaKey || e.ctrlKey) || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) || document.querySelector("[aria-modal=true]")) return;
      const k = e.key.toLowerCase();
      if (k === "z" && !e.shiftKey) {
        e.preventDefault();
        ed.undo();
      } else if ((k === "z" && e.shiftKey) || k === "y") {
        e.preventDefault();
        ed.redo();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [ed.undo, ed.redo]);

  const deleteSelection = useCallback(
    async (wholeTransition = false) => {
      const sel = ed.selection;
      if (!sel || !apply) return;
      if (sel.kind === "status") {
        const { incoming, outgoing } = transitionsOf(ed.doc, sel.name);
        const n = new Set([...incoming, ...outgoing]).size;
        if (
          n > 0 &&
          !(await confirm({
            title: `Delete “${sel.name}”?`,
            body: `${n} transition${n === 1 ? "" : "s"} connected to it will be removed or no longer start from it. You can undo this.`,
            confirmLabel: "Delete status",
            danger: true,
          }))
        )
          return;
        apply((d) => deleteStatus(d, sel.name), { select: null });
      } else {
        const t = ed.doc.transitions[sel.index];
        if (!t) return;
        if (!wholeTransition && sel.from && t.from && t.from.length > 1) apply((d) => removeEdge(d, sel.index, sel.from!), { select: null });
        else apply((d) => deleteTransition(d, sel.index), { select: null });
      }
    },
    [apply, confirm, ed.doc, ed.selection],
  );

  const discard = async () => {
    if (await confirm({ title: "Discard changes?", body: "The editor goes back to the published version.", confirmLabel: "Discard", danger: true })) {
      const d = initialDraft(workflow);
      ed.reset(d.text, d.doc);
      setBaseVersion(workflow.currentVersion);
    }
  };

  const download = () => {
    const url = URL.createObjectURL(new Blob([ed.yaml], { type: "application/yaml" }));
    const a = Object.assign(document.createElement("a"), { href: url, download: `${workflow.name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}.yaml` });
    a.click();
    URL.revokeObjectURL(url);
  };

  const usage = workflow.usage.filter((u) => u.projectKey === projectKey).map((u) => u.issueTypeKey);
  const otherProjects = new Set(workflow.usage.filter((u) => u.projectKey !== projectKey).map((u) => u.projectKey));
  const panelErrors = (prefix: string) => validation.errors.filter((e) => e.path?.startsWith(prefix));
  const sel = ed.selection;
  const selStatusIndex = sel?.kind === "status" ? ed.doc.statuses.findIndex((s) => s.name === sel.name) : -1;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <nav aria-label="Breadcrumb" className="mb-1 flex items-center gap-1 text-[13px] text-muted">
            <Link to="/projects/$projectKey/settings/workflows" params={{ projectKey }} className="hover:text-ink">
              Workflows
            </Link>
            <ChevronRight className="size-3.5" />
            <span className="truncate text-ink">{workflow.name}</span>
          </nav>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-xl font-semibold tracking-tight">{workflow.name}</h1>
            <Badge tone="accent">v{workflow.currentVersion}</Badge>
            {workflow.shared && <Badge>Shared</Badge>}
            {ed.dirty && <Badge tone="warning">Unpublished changes</Badge>}
          </div>
          <p className="mt-1 text-sm text-muted">
            {usage.length ? `Used by ${usage.join(", ")}` : "Not used by any issue type in this project"}
            {otherProjects.size > 0 && ` · also ${otherProjects.size} other project${otherProjects.size === 1 ? "" : "s"}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {canEdit && tab !== "history" && <ValidationBadge pending={validation.pending} errorCount={errorCount} parseError={!!ed.parseError} onClick={() => setTab(ed.parseError ? "yaml" : tab)} />}
          {canEdit && (
            <>
              <div className="flex">
                <IconButton label="Undo (Ctrl+Z)" disabled={!ed.canUndo} onClick={ed.undo}>
                  <Undo2 className="size-4" />
                </IconButton>
                <IconButton label="Redo (Ctrl+Shift+Z)" disabled={!ed.canRedo} onClick={ed.redo}>
                  <Redo2 className="size-4" />
                </IconButton>
              </div>
              <Button disabled={!ed.dirty} onClick={discard}>
                Discard
              </Button>
              <Button
                variant="primary"
                icon={<Rocket className="size-4" />}
                disabled={!ed.dirty || !valid}
                onClick={() => setPublishing(true)}
                title={!valid && ed.dirty ? "Fix the problems first" : undefined}
              >
                Publish version {workflow.currentVersion + 1}
              </Button>
            </>
          )}
        </div>
      </div>

      {!canEdit && (
        <Callout tone="info" icon={<Lock className="mt-0.5 size-4 shrink-0 text-accent" />} title="Shared workflow">
          Shared workflows can be used by every project; only organisation admins can change them. You can view it here.
        </Callout>
      )}
      {workflow.pendingMigration && (
        <Callout tone="info" icon={<Spinner className="mt-0.5 size-4 shrink-0" />} title={`Migrating issues to version ${workflow.currentVersion}`}>
          A background job is moving existing issues to the latest version. Boards and searches update as it progresses.
        </Callout>
      )}
      {workflow.currentVersion > baseVersion && (
        <Callout tone="warning" title={`Version ${workflow.currentVersion} was published while you were editing`}>
          <span className="flex flex-wrap items-center gap-2">
            Your draft is based on version {baseVersion}. Publishing it would replace those changes.
            <Button
              size="sm"
              icon={<RefreshCw className="size-3.5" />}
              onClick={() => {
                const d = initialDraft(workflow);
                ed.reset(d.text, d.doc);
                setBaseVersion(workflow.currentVersion);
              }}
            >
              Load version {workflow.currentVersion}
            </Button>
          </span>
        </Callout>
      )}
      {nameMismatch && !ed.parseError && (
        <Callout tone="danger" title="The workflow name cannot change">
          The document must keep <code>name: {workflow.name}</code>; create a new workflow to use another name.
        </Callout>
      )}

      <div className="overflow-hidden rounded-lg border border-line bg-surface">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line px-2">
          <div role="tablist" aria-label="Editor view" className="flex">
            {(
              [
                ["diagram", "Diagram"],
                ["yaml", "YAML"],
                ["history", "Version history"],
              ] as [Tab, string][]
            ).map(([k, label]) => (
              <button
                key={k}
                role="tab"
                type="button"
                aria-selected={tab === k}
                onClick={() => setTab(k)}
                className={cn("-mb-px border-b-2 px-3 py-2.5 text-sm", tab === k ? "border-accent font-medium text-ink" : "border-transparent text-muted hover:text-ink")}
              >
                {label}
                {k === "yaml" && ed.parseError && <span className="ml-1.5 inline-block size-1.5 rounded-full bg-danger align-middle" aria-label="has errors" />}
              </button>
            ))}
          </div>
          {tab === "yaml" && (
            <div className="flex items-center gap-1.5 py-1.5">
              {ed.handEdited && !ed.parseError && canEdit && (
                <Button
                  size="sm"
                  variant="ghost"
                  icon={<Wand2 className="size-3.5" />}
                  onClick={() => ed.apply((d) => ({ ...d }), { key: "format" })}
                  title="Regenerate the YAML from the diagram (drops comments)"
                >
                  Format
                </Button>
              )}
              <CopyButton value={ed.yaml} variant="ghost" />
              <Button size="sm" variant="ghost" icon={<Download className="size-3.5" />} onClick={download}>
                Download
              </Button>
            </div>
          )}
        </div>

        {tab === "diagram" && (
          <div className="flex h-[calc(100vh-19rem)] min-h-[520px]">
            <div className="relative min-w-0 flex-1">
              {ed.parseError && (
                <div className="absolute inset-x-3 top-3 z-10">
                  <Callout tone="danger" title="The YAML has a syntax error">
                    The diagram shows the last valid version and is read-only until you fix it in the YAML tab.
                  </Callout>
                </div>
              )}
              <WorkflowCanvas
                doc={ed.doc}
                selection={ed.selection}
                onSelect={ed.select}
                apply={apply}
                statusErrors={statusErrors}
                transitionErrors={transitionErrors}
                onDeleteSelection={() => deleteSelection()}
                className="h-full"
              />
            </div>
            <aside
              key={sel ? (sel.kind === "status" ? `s:${sel.name}` : `t:${sel.index}`) : "overview"}
              aria-label="Properties"
              className="w-[340px] shrink-0 overflow-y-auto border-l border-line bg-surface"
            >
              {(validation.errors.length > 0 || nameMismatch) && sel === null && (
                <div className="border-b border-line p-3">
                  <ProblemsList errors={[...(nameMismatch ? [{ path: "name", message: `must stay “${workflow.name}”` }] : []), ...validation.errors]} doc={ed.doc} onSelect={ed.select} />
                </div>
              )}
              {removed.length > 0 && sel === null && (
                <div className="border-b border-line p-3">
                  <Callout tone="warning" title="Issues need a new status">
                    {removed.map((r) => `${r.name} (${r.issueCount} issue${r.issueCount === 1 ? "" : "s"})`).join(", ")} {removed.length === 1 ? "is" : "are"} removed by this draft. You choose where
                    those issues go when you publish.
                  </Callout>
                </div>
              )}
              {sel?.kind === "status" && selStatusIndex >= 0 ? (
                <StatusPanel
                  key={sel.name}
                  doc={ed.doc}
                  name={sel.name}
                  apply={apply}
                  onSelect={ed.select}
                  onDelete={() => deleteSelection()}
                  errors={panelErrors(`statuses[${selStatusIndex}]`)}
                  published={ed.baseline.statuses.some((s) => s.name === sel.name)}
                />
              ) : sel?.kind === "transition" && ed.doc.transitions[sel.index] ? (
                <TransitionPanel key={sel.index} doc={ed.doc} index={sel.index} apply={apply} onDelete={() => deleteSelection(true)} errors={panelErrors(`transitions[${sel.index}]`)} />
              ) : (
                <OverviewPanel doc={ed.doc} apply={apply} onSelect={ed.select} />
              )}
            </aside>
          </div>
        )}

        {tab === "yaml" && (
          <div className="flex flex-col gap-3 p-3">
            {ed.parseError ? (
              <Callout tone="danger" title={`YAML syntax error${ed.parseError.line ? ` on line ${ed.parseError.line}` : ""}`}>
                {ed.parseError.message}
              </Callout>
            ) : (
              <ProblemsList errors={validation.errors} doc={ed.doc} onSelect={(s) => (ed.select(s), setTab("diagram"))} />
            )}
            <YamlEditor value={ed.yaml} onChange={canEdit ? ed.setText : undefined} readOnly={!canEdit} errorLine={ed.parseError?.line} className="h-[calc(100vh-22rem)] min-h-[440px]" />
            <p className="text-xs text-muted">
              Edits here update the diagram as you type, and diagram edits regenerate this YAML (comments are kept until you edit the diagram). The same document works with{" "}
              <code className="font-mono">wfm workflow apply -f workflow.yaml</code>.
            </p>
          </div>
        )}

        {tab === "history" && (
          <div className="p-3">
            <VersionHistory
              workflowId={workflow.id}
              current={workflow.currentVersion}
              canEdit={canEdit}
              onRestore={(yaml, doc) => {
                ed.load(yaml, { ...doc, name: workflow.name });
                setTab("diagram");
              }}
            />
          </div>
        )}
      </div>

      {publishing && (
        <PublishDialog
          open
          onClose={() => setPublishing(false)}
          workflow={workflow}
          yaml={ed.yaml}
          doc={ed.doc}
          baseline={ed.baseline}
          renames={ed.renames}
          removed={removed}
          onPublished={(d) => {
            setPublishing(false);
            const next = initialDraft(d);
            ed.reset(next.text, next.doc);
            setBaseVersion(d.currentVersion);
          }}
        />
      )}
    </div>
  );
}
