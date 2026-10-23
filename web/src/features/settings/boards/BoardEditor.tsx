import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ExternalLink, Trash2 } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Field, Input, Select, Textarea, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { SettingsSection } from "../common";
import { ColumnsEditor, newColumnKey, type ColumnDraft, type StatusInfo } from "./ColumnsEditor";

type Board = Schemas["Board"];

const swimlaneOptions: { value: Schemas["Swimlanes"]; label: string }[] = [
  { value: "none", label: "No swimlanes" },
  { value: "assignee", label: "By assignee" },
  { value: "epic", label: "By epic" },
  { value: "priority", label: "By priority" },
  { value: "type", label: "By issue type" },
];

function draftColumns(b: Board): ColumnDraft[] {
  return b.columns.map((c) => ({ key: newColumnKey(), name: c.name, statusIds: [...c.statusIds], wipLimit: c.wipLimit }));
}

export function BoardEditor({ board, projectKey, statuses, onDeleted }: { board: Board; projectKey: string; statuses: Map<string, StatusInfo>; onDeleted: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [name, setName] = useState(board.name);
  const [type, setType] = useState(board.type);
  const [filterJql, setFilterJql] = useState(board.filterJql);
  const [swimlanes, setSwimlanes] = useState(board.swimlanes);
  const [retention, setRetention] = useState(board.doneRetentionDays);
  const [columns, setColumns] = useState<ColumnDraft[]>(() => draftColumns(board));

  const body: Schemas["BoardInput"] = useMemo(
    () => ({
      name: name.trim(),
      type,
      filterJql: filterJql.trim(),
      swimlanes,
      doneRetentionDays: retention,
      columns: columns.map((c) => ({ name: c.name.trim(), statusIds: c.statusIds, ...(c.wipLimit ? { wipLimit: c.wipLimit } : {}) })),
    }),
    [name, type, filterJql, swimlanes, retention, columns],
  );
  const original = JSON.stringify({
    name: board.name,
    type: board.type,
    filterJql: board.filterJql,
    swimlanes: board.swimlanes,
    doneRetentionDays: board.doneRetentionDays,
    columns: board.columns.map((c) => ({ name: c.name, statusIds: c.statusIds, ...(c.wipLimit ? { wipLimit: c.wipLimit } : {}) })),
  });
  const dirty = JSON.stringify(body) !== original;

  const save = useMutation({
    mutationFn: () => unwrap(api.PUT("/boards/{boardId}", { params: { path: { boardId: board.id } }, body })),
    onSuccess: (b) => {
      qc.setQueryData(["boards", projectKey], (list: Board[] | undefined) => list?.map((x) => (x.id === b.id ? b : x)));
      for (const key of [["boards", projectKey], ["board"], ["backlog"]]) qc.invalidateQueries({ queryKey: key });
      toast.success(`Saved ${b.name}`);
    },
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/boards/{boardId}", { params: { path: { boardId: board.id } } })),
    onSuccess: () => {
      for (const key of [["boards", projectKey], ["board"], ["backlog"]]) qc.invalidateQueries({ queryKey: key });
      toast.success(`Deleted ${board.name}`);
      onDeleted();
    },
    onError: toast.error,
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const reset = () => {
    setName(board.name);
    setType(board.type);
    setFilterJql(board.filterJql);
    setSwimlanes(board.swimlanes);
    setRetention(board.doneRetentionDays);
    setColumns(draftColumns(board));
    save.reset();
  };

  return (
    <form
      className="flex flex-col gap-5"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <SettingsSection
        title="Board"
        actions={
          <Link to="/projects/$projectKey/board" params={{ projectKey }} className="inline-flex items-center gap-1 text-[13px] text-muted hover:text-accent">
            Open board <ExternalLink className="size-3.5" />
          </Link>
        }
      >
        <div className="grid gap-4 p-4 sm:grid-cols-2">
          <Field label="Name" required error={fe.name}>
            {(id) => <Input id={id} value={name} maxLength={80} onChange={(e) => setName(e.target.value)} invalid={!!fe.name} />}
          </Field>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">Type</legend>
            <div role="radiogroup" aria-label="Board type" className="grid grid-cols-2 gap-1.5">
              {(
                [
                  ["scrum", "Scrum", "Shows the active sprint"],
                  ["kanban", "Kanban", "Continuous flow"],
                ] as const
              ).map(([v, label, hint]) => (
                <button
                  key={v}
                  type="button"
                  role="radio"
                  aria-checked={type === v}
                  onClick={() => setType(v)}
                  className={cn("rounded-md border px-3 py-1.5 text-left", type === v ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
                >
                  <span className="block text-sm font-medium">{label}</span>
                  <span className="block text-xs text-muted">{hint}</span>
                </button>
              ))}
            </div>
          </fieldset>
          <Field
            label="Filter (JQL)"
            error={fe.filterJql ?? fe.jql}
            hint="Narrows the board to matching issues of this project, e.g. labels = web AND type != epic. Leave empty for all."
            className="sm:col-span-2"
          >
            {(id) => <Textarea id={id} rows={2} spellCheck={false} value={filterJql} onChange={(e) => setFilterJql(e.target.value)} className="font-mono text-[13px]" placeholder="labels = web" />}
          </Field>
          <Field label="Swimlanes" error={fe.swimlanes}>
            {(id) => (
              <Select id={id} value={swimlanes} onChange={(e) => setSwimlanes(e.target.value as Schemas["Swimlanes"])}>
                {swimlaneOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          {type === "kanban" && (
            <Field label="Hide done issues after" error={fe.doneRetentionDays} hint="Issues resolved longer ago leave the last column.">
              {(id) => (
                <div className="flex items-center gap-2">
                  <Input id={id} type="number" min={1} max={365} value={retention} onChange={(e) => setRetention(Math.min(365, Math.max(1, Number(e.target.value) || 1)))} className="!w-24" />
                  <span className="text-sm text-muted">days</span>
                </div>
              )}
            </Field>
          )}
        </div>
      </SettingsSection>

      <SettingsSection
        title="Columns"
        description="Each status shows in at most one column. Drag statuses between columns, or use the menus. A WIP limit highlights a column when it holds too many issues."
      >
        <div className="p-4">
          <ColumnsEditor columns={columns} onChange={setColumns} statuses={statuses} />
          {fe.columns && <p className="mt-2 text-sm text-danger">{fe.columns}</p>}
        </div>
      </SettingsSection>

      {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" variant="primary" disabled={!dirty || columns.some((c) => !c.name.trim())} loading={save.isPending}>
          Save board
        </Button>
        {dirty && (
          <Button variant="ghost" onClick={reset}>
            Reset
          </Button>
        )}
        <Button
          className="ml-auto"
          icon={<Trash2 className="size-4 text-danger" />}
          loading={remove.isPending}
          onClick={async () => {
            if (
              await confirm({
                title: `Delete ${board.name}?`,
                body: "The board and its column configuration are deleted. Issues and sprints are not affected.",
                confirmLabel: "Delete board",
                danger: true,
              })
            )
              remove.mutate();
          }}
        >
          Delete board
        </Button>
      </div>
    </form>
  );
}
