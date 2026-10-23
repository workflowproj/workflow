import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ChevronDown, Search, X } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { issueTypesQuery, labelsQuery, projectsQuery } from "@/api/queries";
import { PRIORITIES, type Issue, type IssueType, type Priority, type User } from "@/api/types";
import { Button, Checkbox, Field, Input, Modal, Popover, Select, Spinner, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon, PriorityIcon, priorityLabel, StatusBadge } from "@/components/issue-bits";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { UserPicker } from "@/components/UserPicker";
import { cn } from "@/lib/cn";

type Defaults = { typeKey?: string; sprintId?: string; parentKey?: string; statusId?: string };
type ParentRef = Pick<Issue, "key" | "summary" | "type" | "status">;

/**
 * CreateIssueDialog creates an issue. Opened from the top bar ("Create"), boards and backlogs.
 * `projectKey` preselects the project; `defaults` pre-fills fields (e.g. sprintId, parentKey).
 */
export function CreateIssueDialog(props: {
  open: boolean;
  onClose: () => void;
  projectKey?: string;
  defaults?: Defaults;
  onCreated?: (issue: { key: string }) => void;
}) {
  // Mounting the form only while open gives every opening fresh state.
  return (
    <Modal open={props.open} onClose={props.onClose} title="Create issue" size="lg">
      {props.open && <CreateIssueForm {...props} />}
    </Modal>
  );
}

const search = (jql: string, limit = 20) => unwrap(api.GET("/search", { params: { query: { jql, limit } } }));

function CreateIssueForm({ onClose, projectKey: initialProject, defaults, onCreated }: { onClose: () => void; projectKey?: string; defaults?: Defaults; onCreated?: (issue: { key: string }) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();
  const { data: projects, isLoading: loadingProjects } = useQuery(projectsQuery);
  const creatable = useMemo(() => (projects ?? []).filter((p) => p.myPermissions.includes("issue.create")), [projects]);
  const [projectKey, setProjectKey] = useState(initialProject ?? "");
  const project = creatable.find((p) => p.key === projectKey) ?? creatable.find((p) => p.key === initialProject) ?? creatable[0];
  const key = project?.key ?? "";

  const { data: types = [] } = useQuery({ ...issueTypesQuery(key), enabled: !!key });
  const { data: sprints = [] } = useQuery({
    queryKey: ["sprints", key],
    queryFn: () => unwrap(api.GET("/projects/{projectKey}/sprints", { params: { path: { projectKey: key } } })),
    enabled: !!key,
    staleTime: 30_000,
  });
  const { data: knownLabels = [] } = useQuery({ ...labelsQuery(key), enabled: !!key });

  const [typeKey, setTypeKey] = useState(defaults?.typeKey ?? "");
  const [summary, setSummary] = useState("");
  const [description, setDescription] = useState("");
  const [assignee, setAssignee] = useState<User | null>(null);
  const [priority, setPriority] = useState<Priority>("medium");
  const [labels, setLabels] = useState<string[]>([]);
  const [points, setPoints] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [sprintId, setSprintId] = useState(defaults?.sprintId ?? "");
  const [parent, setParent] = useState<ParentRef | null>(null);
  const [another, setAnother] = useState(false);
  const [lastCreated, setLastCreated] = useState<string | null>(null);
  const summaryRef = useRef<HTMLInputElement>(null);

  const type = types.find((t) => t.key === typeKey) ?? types.find((t) => t.key === "story") ?? types.find((t) => t.level === 0) ?? types[0];
  const openSprints = sprints.filter((s) => s.state !== "closed");

  // The default parent (e.g. "add sub-task" from an issue) is looked up once for its summary.
  const { data: defaultParent } = useQuery({
    queryKey: ["search", `key = ${defaults?.parentKey}`, "picker"],
    queryFn: () => search(`key = ${defaults!.parentKey}`, 1),
    enabled: !!defaults?.parentKey,
  });
  const appliedParent = useRef(false);
  useEffect(() => {
    const p = defaultParent?.issues[0];
    const subtask = types.find((t) => t.level === -1);
    if (!p || appliedParent.current || (p.type.level === 0 && !defaults?.typeKey && !subtask)) return;
    appliedParent.current = true;
    setParent(p);
    // A standard parent means the new issue is a sub-task.
    if (!defaults?.typeKey && p.type.level === 0) setTypeKey(subtask!.key);
  }, [defaultParent, defaults?.typeKey, types]);

  const changeProject = (k: string) => {
    setProjectKey(k);
    setTypeKey("");
    setParent(null);
    setSprintId("");
    setAssignee(null);
  };
  const changeType = (t: IssueType) => {
    setTypeKey(t.key);
    if (parent && parent.type.level !== t.level + 1) setParent(null);
  };

  const create = useMutation({
    mutationFn: async () => {
      const level = type!.level;
      const issue = await unwrap(
        api.POST("/issues", {
          body: {
            projectKey: key,
            typeKey: type!.key,
            summary: summary.trim(),
            description: description.trim() || undefined,
            priority,
            assigneeId: assignee?.id,
            parentKey: level <= 0 ? parent?.key : undefined,
            sprintId: level === 0 && sprintId ? sprintId : undefined,
            storyPoints: points === "" ? undefined : Number(points),
            dueDate: dueDate || undefined,
            labels: labels.length ? labels : undefined,
          },
        }),
      );
      // A board column's "create here" asks for a status; creation itself always starts in the initial one.
      if (defaults?.statusId && issue.status.id !== defaults.statusId) {
        await unwrap(api.POST("/issues/{issueKey}/move", { params: { path: { issueKey: issue.key } }, body: { statusId: defaults.statusId } })).catch(() =>
          toast.info(`${issue.key} was created in ${issue.status.name}; the workflow didn't allow moving it straight away.`),
        );
      }
      return issue;
    },
    onSuccess: (issue) => {
      for (const k of [["board"], ["backlog"], ["search"], ["labels", key], ["releases", key]]) qc.invalidateQueries({ queryKey: k });
      toast.success(`${issue.key} created`, { label: "Open", onClick: () => navigate({ to: "/browse/$issueKey", params: { issueKey: issue.key } }) });
      onCreated?.({ key: issue.key });
      if (another) {
        setSummary("");
        setDescription("");
        setLastCreated(issue.key);
        create.reset();
        summaryRef.current?.focus();
      } else onClose();
    },
  });

  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  const shownFields = ["projectKey", "typeKey", "summary", "parentKey", "assigneeId", "priority", "sprintId", "storyPoints", "dueDate", "labels"];
  const otherError = create.error && !Object.keys(fe).some((f) => shownFields.includes(f));
  const canSubmit = !!project && !!type && summary.trim() !== "" && !(type.level < 0 && !parent);
  const submit = () => canSubmit && !create.isPending && create.mutate();
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      submit();
    }
  };

  if (loadingProjects) return <Spinner className="mx-auto my-8 size-5" />;
  if (!project) return <p className="py-4 text-sm text-muted">You don't have permission to create issues in any project.</p>;

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      onKeyDown={onKeyDown}
      className="flex flex-col gap-4"
    >
      {lastCreated && (
        <div role="status" className="flex items-center gap-2 rounded-md border border-success/30 bg-success-soft px-3 py-2 text-sm">
          <CheckCircle2 className="size-4 text-success" />
          <span className="flex-1">
            <Link to="/browse/$issueKey" params={{ issueKey: lastCreated }} className="font-medium text-accent hover:underline" onClick={onClose}>
              {lastCreated}
            </Link>{" "}
            was created. Add the next one.
          </span>
        </div>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Project" required error={fe.projectKey}>
          {(id) => (
            <Select id={id} value={project.key} onChange={(e) => changeProject(e.target.value)}>
              {creatable.map((p) => (
                <option key={p.id} value={p.key}>
                  {p.name} ({p.key})
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="Issue type" required error={fe.typeKey}>
          {(id) => (
            <span className="relative block">
              {type && (
                <span aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 flex -translate-y-1/2">
                  <IssueTypeIcon type={type} />
                </span>
              )}
              <Select id={id} value={type?.key ?? ""} onChange={(e) => changeType(types.find((t) => t.key === e.target.value)!)} className="pl-8.5">
                {[...types]
                  .sort((a, b) => b.level - a.level || a.position - b.position)
                  .map((t) => (
                    <option key={t.id} value={t.key}>
                      {t.name}
                    </option>
                  ))}
              </Select>
            </span>
          )}
        </Field>
      </div>
      {type && type.level <= 0 && (
        <Field
          label={type.level < 0 ? "Parent issue" : "Epic"}
          required={type.level < 0}
          error={fe.parentKey}
          hint={type.level < 0 ? `A ${type.name.toLowerCase()} belongs to a standard issue.` : undefined}
        >
          {(id) => <ParentPicker id={id} projectKey={project.key} level={type.level + 1} value={parent} onChange={setParent} invalid={!!fe.parentKey} />}
        </Field>
      )}
      <Field label="Summary" required error={fe.summary}>
        {(id) => <Input id={id} ref={summaryRef} autoFocus value={summary} maxLength={255} onChange={(e) => setSummary(e.target.value)} invalid={!!fe.summary} placeholder="What needs to be done?" />}
      </Field>
      <div className="flex flex-col gap-1.5">
        <span className="text-[13px] font-medium">Description</span>
        <MarkdownEditor value={description} onChange={setDescription} onSubmit={submit} placeholder="Add more detail, steps to reproduce, acceptance criteria…" minHeight={110} />
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Assignee" error={fe.assigneeId}>
          {() => <UserPicker value={assignee} onChange={setAssignee} project={project.key} label="Assignee" />}
        </Field>
        <Field label="Priority" error={fe.priority}>
          {(id) => (
            <span className="relative block">
              <span aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 flex -translate-y-1/2">
                <PriorityIcon priority={priority} />
              </span>
              <Select id={id} value={priority} onChange={(e) => setPriority(e.target.value as Priority)} className="pl-8.5">
                {PRIORITIES.map((p) => (
                  <option key={p} value={p}>
                    {priorityLabel(p)}
                  </option>
                ))}
              </Select>
            </span>
          )}
        </Field>
      </div>
      <div className={cn("grid gap-4", type?.level === 0 && openSprints.length > 0 ? "sm:grid-cols-[1fr_7rem_10rem]" : "sm:grid-cols-[7rem_10rem]")}>
        {type?.level === 0 && openSprints.length > 0 && (
          <Field label="Sprint" error={fe.sprintId}>
            {(id) => (
              <Select id={id} value={sprintId} onChange={(e) => setSprintId(e.target.value)}>
                <option value="">Backlog</option>
                {openSprints.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                    {s.state === "active" ? " (active)" : ""}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}
        <Field label="Story points" error={fe.storyPoints}>
          {(id) => <Input id={id} type="number" inputMode="decimal" min={0} step={0.5} value={points} onChange={(e) => setPoints(e.target.value)} invalid={!!fe.storyPoints} />}
        </Field>
        <Field label="Due date" error={fe.dueDate}>
          {(id) => <Input id={id} type="date" value={dueDate} onChange={(e) => setDueDate(e.target.value)} invalid={!!fe.dueDate} />}
        </Field>
      </div>
      <Field label="Labels" error={fe.labels} hint="Press Enter or comma to add a label.">
        {(id) => <LabelsInput id={id} value={labels} onChange={setLabels} suggestions={knownLabels} />}
      </Field>
      {otherError && <ErrorBox error={create.error} />}
      <div className="-mx-5 -mb-4 flex items-center justify-end gap-2 border-t border-line px-5 py-3">
        <Checkbox label="Create another" checked={another} onChange={(e) => setAnother(e.target.checked)} className="mr-auto" />
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" type="submit" loading={create.isPending} disabled={!canSubmit} title="Create (Ctrl+Enter)">
          Create
        </Button>
      </div>
    </form>
  );
}

/** ParentPicker searches the project's issues one level up (epics for standard issues, standard issues for sub-tasks). */
function ParentPicker({ id, projectKey, level, value, onChange, invalid }: { id: string; projectKey: string; level: number; value: ParentRef | null; onChange: (p: ParentRef | null) => void; invalid?: boolean }) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 200);
    return () => clearTimeout(t);
  }, [q]);
  const text = /^[A-Za-z][A-Za-z0-9_]{1,9}-\d+$/.test(debounced) ? `key = ${debounced.toUpperCase()}` : debounced ? `text ~ "${debounced.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"` : "";
  const jql = `project = ${projectKey} AND level = ${level}${text ? ` AND ${text}` : ""} ORDER BY updated DESC`;
  const { data, isFetching, error } = useQuery({ queryKey: ["search", jql, "picker"], queryFn: () => search(jql), enabled: open, placeholderData: keepPreviousData });
  const what = level >= 1 ? "epic" : "issue";
  const results = data?.issues ?? [];
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      className="w-[min(30rem,calc(100vw-3rem))]"
      trigger={({ toggle }) => (
        <span className="flex items-center gap-1">
          <button
            id={id}
            type="button"
            onClick={toggle}
            aria-haspopup="listbox"
            aria-expanded={open}
            className={cn(
              "flex h-8.5 w-full min-w-0 items-center gap-2 rounded-md border bg-surface px-2.5 text-left text-sm focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none",
              invalid ? "border-danger" : "border-line-strong",
            )}
          >
            {value ? (
              <>
                <IssueTypeIcon type={value.type} />
                <span className="font-mono text-xs text-muted">{value.key}</span>
                <span className="min-w-0 flex-1 truncate">{value.summary}</span>
              </>
            ) : (
              <span className="flex-1 text-faint">Select {what}…</span>
            )}
            <ChevronDown className="size-3.5 shrink-0 text-faint" />
          </button>
          {value && (
            <button type="button" aria-label={`Remove ${what}`} onClick={() => onChange(null)} className="rounded p-1 text-faint hover:bg-surface-2 hover:text-ink">
              <X className="size-4" />
            </button>
          )}
        </span>
      )}
      content={(close) => (
        <div>
          <div className="relative mb-1">
            <Search className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-faint" />
            <input
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={(e) => {
                // Escape closes the picker only, not the dialog around it.
                if (e.key === "Escape") {
                  e.nativeEvent.stopPropagation();
                  close();
                }
              }}
              placeholder={`Search ${what}s by key or summary`}
              aria-label={`Search ${what}s`}
              className="h-8 w-full rounded border border-line bg-surface pr-2 pl-7 text-sm focus:border-accent focus:outline-none"
            />
          </div>
          <ul role="listbox" className="max-h-64 overflow-y-auto">
            {error && <li className="px-2 py-2 text-sm text-danger">{(error as Error).message}</li>}
            {results.map((i) => (
              <li key={i.id}>
                <button
                  type="button"
                  role="option"
                  aria-selected={value?.key === i.key}
                  onClick={() => {
                    onChange(i);
                    close();
                  }}
                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-surface-2"
                >
                  <IssueTypeIcon type={i.type} />
                  <span className="font-mono text-xs text-muted">{i.key}</span>
                  <span className="min-w-0 flex-1 truncate">{i.summary}</span>
                  <StatusBadge status={i.status} className="max-w-[7rem]" />
                </button>
              </li>
            ))}
            {!isFetching && !error && results.length === 0 && <li className="px-2 py-2 text-sm text-muted">No {what}s found</li>}
            {isFetching && results.length === 0 && (
              <li className="flex items-center gap-2 px-2 py-2 text-sm text-muted">
                <Spinner /> Searching…
              </li>
            )}
          </ul>
        </div>
      )}
    />
  );
}

/** LabelsInput edits a list of labels as chips; spaces become dashes like the API does. */
function LabelsInput({ id, value, onChange, suggestions }: { id: string; value: string[]; onChange: (v: string[]) => void; suggestions: string[] }) {
  const [text, setText] = useState("");
  const add = (raw: string) => {
    const l = raw.trim().split(/\s+/).join("-").slice(0, 50);
    if (l && !value.includes(l)) onChange([...value, l]);
    setText("");
  };
  return (
    <div className="flex min-h-8.5 flex-wrap items-center gap-1 rounded-md border border-line-strong bg-surface px-1.5 py-1 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent/25">
      {value.map((l) => (
        <span key={l} className="inline-flex h-6 items-center gap-0.5 rounded bg-surface-2 pr-0.5 pl-2 text-xs font-medium">
          {l}
          <button type="button" aria-label={`Remove label ${l}`} onClick={() => onChange(value.filter((x) => x !== l))} className="rounded p-0.5 text-faint hover:bg-surface-3 hover:text-ink">
            <X className="size-3" />
          </button>
        </span>
      ))}
      <input
        id={id}
        list={`${id}-labels`}
        value={text}
        onChange={(e) => (e.target.value.endsWith(",") ? add(e.target.value.slice(0, -1)) : setText(e.target.value))}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !(e.metaKey || e.ctrlKey) && text.trim()) {
            e.preventDefault();
            add(text);
          } else if (e.key === "Backspace" && !text && value.length) onChange(value.slice(0, -1));
        }}
        onBlur={() => text.trim() && add(text)}
        placeholder={value.length ? "" : "Add labels"}
        className="h-6 min-w-28 flex-1 bg-transparent px-1 text-sm placeholder:text-faint focus:outline-none"
      />
      <datalist id={`${id}-labels`}>
        {suggestions
          .filter((s) => !value.includes(s))
          .map((s) => (
            <option key={s} value={s} />
          ))}
      </datalist>
    </div>
  );
}
