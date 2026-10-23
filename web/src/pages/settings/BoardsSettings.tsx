import { useMemo, useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KanbanSquare, Plus } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { statusesQuery } from "@/api/queries";
import { Badge, Button, EmptyState, Field, Input, Modal, PageHeader, PageSpinner, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { BoardEditor } from "@/features/settings/boards/BoardEditor";
import type { StatusInfo } from "@/features/settings/boards/ColumnsEditor";
import { boardsQuery, useProjectStatuses } from "@/features/settings/queries";
import { cn } from "@/lib/cn";

const route = getRouteApi("/app/projects/$projectKey/settings/boards");

export function BoardsSettings() {
  const { projectKey } = route.useParams();
  const { data: boards = [], isLoading, error } = useQuery(boardsQuery(projectKey));
  const project = useProjectStatuses(projectKey);
  const { data: all = [] } = useQuery(statusesQuery);
  const [picked, setPicked] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const selected = boards.find((b) => b.id === picked) ?? boards[0];

  // Every status a column might reference: the project's (in workflow order) plus stale ones.
  const statuses = useMemo(() => {
    const m = new Map<string, StatusInfo>();
    for (const s of project.statuses) m.set(s.id, { id: s.id, name: s.name, category: s.category, inProject: true });
    for (const s of all) if (!m.has(s.id)) m.set(s.id, { id: s.id, name: s.name, category: s.category, inProject: false });
    return m;
  }, [project.statuses, all]);

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Boards"
        subtitle="Columns, filters and swimlanes of the project's boards."
        actions={
          <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setCreating(true)}>
            New board
          </Button>
        }
      />
      {(error || project.error) && <ErrorBox error={error ?? project.error} />}
      {isLoading || project.loading ? (
        <PageSpinner />
      ) : boards.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState icon={<KanbanSquare className="size-8" />} title="No boards" body="Create a board to visualise work by status." action={<Button onClick={() => setCreating(true)}>New board</Button>} />
        </div>
      ) : (
        <div className="flex flex-col gap-5">
          {boards.length > 1 && (
            <div role="tablist" aria-label="Boards" className="flex flex-wrap gap-2">
              {boards.map((b) => (
                <button
                  key={b.id}
                  role="tab"
                  type="button"
                  aria-selected={selected?.id === b.id}
                  onClick={() => setPicked(b.id)}
                  className={cn("flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm", selected?.id === b.id ? "border-accent bg-accent-soft font-medium text-accent" : "border-line bg-surface hover:border-line-strong")}
                >
                  {b.name}
                  <Badge>{b.type}</Badge>
                </button>
              ))}
            </div>
          )}
          {selected && <BoardEditor key={selected.id} board={selected} projectKey={projectKey} statuses={statuses} onDeleted={() => setPicked(null)} />}
        </div>
      )}
      {creating && (
        <CreateBoardDialog
          projectKey={projectKey}
          onClose={() => setCreating(false)}
          onCreated={(id) => {
            setPicked(id);
            setCreating(false);
          }}
        />
      )}
    </div>
  );
}

function CreateBoardDialog({ projectKey, onClose, onCreated }: { projectKey: string; onClose: () => void; onCreated: (id: string) => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [type, setType] = useState<"scrum" | "kanban">("kanban");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/projects/{projectKey}/boards", { params: { path: { projectKey } }, body: { name: name.trim(), type } })),
    onSuccess: async (b) => {
      await Promise.all([["boards", projectKey], ["board"], ["backlog"]].map((key) => qc.invalidateQueries({ queryKey: key })));
      onCreated(b.id);
    },
  });
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  return (
    <Modal
      open
      onClose={onClose}
      title="New board"
      size="sm"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="new-board" disabled={!name.trim()} loading={create.isPending}>
            Create board
          </Button>
        </>
      }
    >
      <form
        id="new-board"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) create.mutate();
        }}
      >
        <Field label="Name" required error={fe.name}>
          {(id) => <Input id={id} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Team board" />}
        </Field>
        <Field label="Type" hint="New boards get one column per status the project's workflows use; adjust them afterwards.">
          {(id) => (
            <Select id={id} value={type} onChange={(e) => setType(e.target.value as "scrum" | "kanban")}>
              <option value="kanban">Kanban</option>
              <option value="scrum">Scrum</option>
            </Select>
          )}
        </Field>
        {create.error && Object.keys(fe).length === 0 && <ErrorBox error={create.error} />}
      </form>
    </Modal>
  );
}
