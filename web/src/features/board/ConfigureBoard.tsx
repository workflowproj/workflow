import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ExternalLink, X } from "lucide-react";
import { ApiError } from "@/api/client";
import { Button, Drawer, IconButton, PageSpinner, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { columnErrors, columnInputs, draftColumns, withChanges, type ColumnDraft } from "./board-config";
import { CardFieldsEditor } from "./CardFieldsEditor";
import { normalizeCardFields, type CardField } from "./card-fields";
import { ColumnsEditor, useStatusInfo } from "./ColumnsEditor";
import type { Board } from "./queries";
import { useSaveBoard } from "./useBoardConfig";

export type ConfigureTab = "columns" | "cards";

const tabs: { id: ConfigureTab; label: string }[] = [
  { id: "columns", label: "Columns" },
  { id: "cards", label: "Card layout" },
];

/**
 * ConfigureBoard is the board's own settings drawer: columns (names, order, statuses, WIP
 * limits) and what cards show. Edits are drafts until saved; the board then updates at once
 * and refetches. Filter, swimlane defaults and the rest stay on the settings page.
 */
export function ConfigureBoard({ board, issueCounts, initialTab = "columns", onClose }: { board: Board; issueCounts?: Map<string, number>; initialTab?: ConfigureTab; onClose: () => void }) {
  const [tab, setTab] = useState<ConfigureTab>(initialTab);
  const [initial] = useState(() => ({ columns: draftColumns(board.columns), cardFields: normalizeCardFields(board.cardFields) }));
  const [columns, setColumns] = useState<ColumnDraft[]>(initial.columns);
  const [cardFields, setCardFields] = useState<CardField[]>(initial.cardFields);
  const { statuses, loading, error: statusError } = useStatusInfo(board.projectKey);
  const save = useSaveBoard(board.projectKey, board.id);
  const toast = useToast();
  const confirm = useConfirm();

  const snapshot = (c: ColumnDraft[], f: CardField[]) => JSON.stringify([columnInputs(c), f]);
  const dirty = snapshot(columns, cardFields) !== snapshot(initial.columns, initial.cardFields);
  const unnamed = columns.some((c) => !c.name.trim());
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const colErrors = columnErrors(fe);
  const otherError = save.error && !colErrors.general && Object.keys(colErrors.byIndex).length === 0 && !fe.cardFields;

  const close = async () => {
    if (save.isPending) return;
    if (dirty && !(await confirm({ title: "Discard your changes?", body: "Your changes to the board's columns and cards haven't been saved.", confirmLabel: "Discard", danger: true }))) return;
    onClose();
  };
  const submit = async () => {
    try {
      const saved = await save.mutateAsync(withChanges(board, { columns, cardFields }));
      toast.success(`Saved ${saved.name}`);
      onClose();
    } catch (err) {
      const f = err instanceof ApiError ? err.fieldErrors() : {};
      if (f.cardFields && !Object.keys(f).some((k) => k.startsWith("columns"))) setTab("cards");
      else if (Object.keys(f).some((k) => k.startsWith("columns"))) setTab("columns");
    }
  };

  return (
    <Drawer open onClose={close} label="Configure board" width="max-w-5xl">
      <div className="flex min-h-full flex-col">
        <header className="sticky top-0 z-10 border-b border-line bg-surface px-5 pt-4">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <h2 className="text-base font-semibold">Configure board</h2>
              <p className="truncate text-[13px] text-muted">{board.name} · changes apply to everyone when you save</p>
            </div>
            <IconButton label="Close" size="sm" onClick={close} data-close>
              <X className="size-4" />
            </IconButton>
          </div>
          <div role="tablist" aria-label="Board configuration" className="mt-3 -mb-px flex gap-1">
            {tabs.map((t) => (
              <button
                key={t.id}
                type="button"
                role="tab"
                id={`configure-${t.id}`}
                aria-selected={tab === t.id}
                aria-controls="configure-panel"
                onClick={() => setTab(t.id)}
                className={cn(
                  "border-b-2 px-3 py-2 text-sm focus-visible:outline-2 focus-visible:outline-accent",
                  tab === t.id ? "border-accent font-medium text-ink" : "border-transparent text-muted hover:text-ink",
                )}
              >
                {t.label}
                {t.id === "columns" && (colErrors.general || Object.keys(colErrors.byIndex).length > 0) && <span className="ml-1.5 inline-block size-1.5 rounded-full bg-danger align-middle" />}
              </button>
            ))}
          </div>
        </header>
        <div id="configure-panel" role="tabpanel" aria-labelledby={`configure-${tab}`} className="flex-1 px-5 py-4">
          {tab === "columns" ? (
            <>
              <p className="mb-3 text-[13px] text-muted">
                Each status shows in at most one column. Drag columns by their handle and statuses by their chips, or use the buttons and menus. A WIP limit turns a column red when it holds too
                many issues.
              </p>
              {statusError ? (
                <ErrorBox error={statusError} />
              ) : loading ? (
                <PageSpinner label="Loading statuses" />
              ) : (
                <ColumnsEditor columns={columns} onChange={setColumns} statuses={statuses} issueCounts={issueCounts} errors={colErrors.byIndex} />
              )}
              {colErrors.general && <p className="mt-2 text-sm text-danger">{colErrors.general}</p>}
            </>
          ) : (
            <CardFieldsEditor value={cardFields} onChange={setCardFields} projectKey={board.projectKey} error={fe.cardFields} />
          )}
        </div>
        <footer className="sticky bottom-0 flex flex-col gap-2 border-t border-line bg-surface px-5 py-3">
          {otherError && <ErrorBox error={save.error} />}
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="primary" disabled={!dirty || unnamed} loading={save.isPending} onClick={submit}>
              Save changes
            </Button>
            <Button variant="ghost" onClick={close} disabled={save.isPending}>
              Cancel
            </Button>
            {unnamed && <span className="text-[13px] text-danger">Every column needs a name.</span>}
            <Link
              to="/projects/$projectKey/settings/boards"
              params={{ projectKey: board.projectKey }}
              className="ml-auto inline-flex items-center gap-1 text-[13px] text-muted hover:text-accent"
            >
              Filter, swimlanes and more <ExternalLink className="size-3.5" />
            </Link>
          </div>
        </footer>
      </div>
    </Drawer>
  );
}
