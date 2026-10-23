import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bug, Clock, Inbox, List, Pencil, PenLine, Star, Trash2, Users } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { Button, Checkbox, Field, Input, Modal, Spinner, Textarea, useConfirm, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { filtersQuery } from "./search-api";

export type SavedFilter = Schemas["SavedFilter"];

const norm = (s: string) => s.trim().replace(/\s+/g, " ").toLowerCase();
export const sameJql = (a: string, b: string) => norm(a) === norm(b);

/** Built-in starting points; project pages scope them to the project. */
export function presets(projectKey?: string) {
  const scope = (q: string) => (projectKey ? `project = ${projectKey}${q.startsWith("ORDER") ? " " : " AND "}${q}` : q);
  return [
    { id: "mine", label: "My open issues", icon: Inbox, jql: scope("assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC") },
    { id: "reported", label: "Reported by me", icon: PenLine, jql: scope("reporter = currentUser() ORDER BY created DESC") },
    { id: "recent", label: "Updated recently", icon: Clock, jql: scope("updated >= -7d ORDER BY updated DESC") },
    { id: "bugs", label: "Open bugs", icon: Bug, jql: scope("type = Bug AND statusCategory != Done ORDER BY priority DESC") },
    { id: "all", label: "All issues", icon: List, jql: scope("ORDER BY updated DESC") },
  ];
}

/**
 * FiltersPanel lists the quick filters, your saved filters and filters others shared.
 * Own filters can be edited or deleted from here.
 */
export function FiltersPanel({
  projectKey,
  currentJql,
  activeId,
  onPreset,
  onFilter,
  onDeleted,
}: {
  projectKey?: string;
  currentJql: string;
  activeId?: string;
  onPreset: (jql: string) => void;
  onFilter: (f: SavedFilter) => void;
  onDeleted: (f: SavedFilter) => void;
}) {
  const { data: filters = [], isLoading } = useQuery(filtersQuery);
  const [editing, setEditing] = useState<SavedFilter | null>(null);
  const mine = filters.filter((f) => f.mine);
  const shared = filters.filter((f) => !f.mine);
  return (
    <nav aria-label="Filters" className="flex flex-col gap-4 text-sm">
      <Group title="Quick filters">
        {presets(projectKey).map((p) => (
          <Item key={p.id} icon={<p.icon className="size-4" />} active={!activeId && sameJql(p.jql, currentJql)} onClick={() => onPreset(p.jql)}>
            {p.label}
          </Item>
        ))}
      </Group>
      <Group title="My filters">
        {isLoading && <Spinner className="mx-2.5 size-3.5" />}
        {!isLoading && mine.length === 0 && <p className="px-2.5 text-[13px] text-faint">Save a search to find it here.</p>}
        {mine.map((f) => (
          <FilterItem key={f.id} filter={f} active={activeId === f.id} onOpen={() => onFilter(f)} onEdit={() => setEditing(f)} onDeleted={() => onDeleted(f)} />
        ))}
      </Group>
      {shared.length > 0 && (
        <Group title="Shared with everyone">
          {shared.map((f) => (
            <FilterItem key={f.id} filter={f} active={activeId === f.id} onOpen={() => onFilter(f)} />
          ))}
        </Group>
      )}
      <SaveFilterDialog open={!!editing} filter={editing ?? undefined} jql={editing?.jql ?? ""} onClose={() => setEditing(null)} />
    </nav>
  );
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div>
      <p className="px-2.5 pb-1 text-[11px] font-semibold tracking-wide text-faint uppercase">{title}</p>
      <div className="flex flex-col gap-0.5">{children}</div>
    </div>
  );
}

function Item({ icon, active, onClick, children, title }: { icon: ReactNode; active: boolean; onClick: () => void; children: ReactNode; title?: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-current={active ? "true" : undefined}
      className={cn(
        "flex w-full min-w-0 items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left",
        active ? "bg-accent-soft font-medium text-accent" : "text-muted hover:bg-surface-2 hover:text-ink",
      )}
    >
      <span className="shrink-0">{icon}</span>
      <span className="truncate">{children}</span>
    </button>
  );
}

function FilterItem({ filter: f, active, onOpen, onEdit, onDeleted }: { filter: SavedFilter; active: boolean; onOpen: () => void; onEdit?: () => void; onDeleted?: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/filters/{filterId}", { params: { path: { filterId: f.id } } })),
    onSuccess: () => {
      qc.setQueryData<SavedFilter[]>(["filters"], (list) => list?.filter((x) => x.id !== f.id));
      qc.invalidateQueries({ queryKey: ["filters"] });
      toast.success(`Deleted "${f.name}"`);
      onDeleted?.();
    },
    onError: (err) => toast.error(err),
  });
  const title = [f.description, f.mine ? undefined : `by ${f.owner.displayName}`, f.jql].filter(Boolean).join("\n");
  return (
    <div className="group relative">
      <Item icon={f.shared ? <Users className="size-4" /> : <Star className="size-4" />} active={active} onClick={onOpen} title={title}>
        <span className="pr-12">{f.name}</span>
      </Item>
      {f.mine && (
        <div className="absolute top-1/2 right-1 flex -translate-y-1/2 gap-0.5 opacity-0 group-hover:opacity-100 focus-within:opacity-100">
          <button type="button" aria-label={`Edit ${f.name}`} title="Rename or change sharing" onClick={onEdit} className="rounded p-1 text-faint hover:bg-surface-3 hover:text-ink">
            <Pencil className="size-3.5" />
          </button>
          <button
            type="button"
            aria-label={`Delete ${f.name}`}
            title="Delete filter"
            disabled={remove.isPending}
            onClick={async () => {
              if (await confirm({ title: "Delete filter?", body: `"${f.name}" will be deleted${f.shared ? " for everyone it is shared with" : ""}.`, confirmLabel: "Delete", danger: true })) remove.mutate();
            }}
            className="rounded p-1 text-faint hover:bg-surface-3 hover:text-danger"
          >
            <Trash2 className="size-3.5" />
          </button>
        </div>
      )}
    </div>
  );
}

/**
 * SaveFilterDialog saves `jql` as a new filter, or (with `filter`) updates that filter's name,
 * description and sharing — and its query when `jql` differs.
 */
export function SaveFilterDialog({ open, onClose, jql, filter, onSaved }: { open: boolean; onClose: () => void; jql: string; filter?: SavedFilter; onSaved?: (f: SavedFilter) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [shared, setShared] = useState(false);
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), description: description.trim(), jql, shared };
      return filter
        ? unwrap(api.PUT("/filters/{filterId}", { params: { path: { filterId: filter.id } }, body }))
        : unwrap(api.POST("/filters", { body }));
    },
    onSuccess: (f) => {
      qc.invalidateQueries({ queryKey: ["filters"] });
      toast.success(filter ? `Saved "${f.name}"` : `Filter "${f.name}" created`);
      onSaved?.(f);
      onClose();
    },
  });
  useEffect(() => {
    if (!open) return;
    setName(filter?.name ?? "");
    setDescription(filter?.description ?? "");
    setShared(filter?.shared ?? false);
    save.reset();
  }, [open, filter?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={filter ? "Edit filter" : "Save filter"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="save-filter" loading={save.isPending} disabled={!name.trim()}>
            {filter ? "Save" : "Save filter"}
          </Button>
        </>
      }
    >
      <form
        id="save-filter"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name" required error={fe.name}>
          {(id) => <Input id={id} autoFocus value={name} maxLength={120} onChange={(e) => setName(e.target.value)} placeholder="e.g. Blocked bugs this sprint" />}
        </Field>
        <Field label="Description">{(id) => <Textarea id={id} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}</Field>
        <div>
          <p className="mb-1.5 text-[13px] font-medium">Query</p>
          <code className="block rounded-md bg-surface-2 px-3 py-2 font-mono text-xs break-words text-muted">{jql || "(all issues)"}</code>
        </div>
        <Checkbox label="Share with everyone in the organisation" checked={shared} onChange={(e) => setShared(e.target.checked)} />
        {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}
