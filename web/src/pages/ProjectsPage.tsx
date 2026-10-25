import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderPlus, Globe, Lock } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { projectsQuery } from "@/api/queries";
import { Badge, Button, EmptyState, Field, Input, Modal, PageHeader, PageSpinner, Select, Textarea, table } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserAvatar } from "@/components/UserAvatar";
import { ProjectAvatar } from "@/layout/AppShell";
import { cn } from "@/lib/cn";

export function ProjectsPage() {
  const { data: projects, isLoading, error } = useQuery(projectsQuery);
  const [creating, setCreating] = useState(false);
  const [filter, setFilter] = useState("");
  const shown = (projects ?? []).filter((p) => `${p.key} ${p.name}`.toLowerCase().includes(filter.toLowerCase()));
  return (
    <div className="mx-auto max-w-6xl p-6">
      <PageHeader
        title="Projects"
        actions={
          <Button variant="primary" icon={<FolderPlus className="size-4" />} onClick={() => setCreating(true)}>
            New project
          </Button>
        }
      />
      {error && <ErrorBox error={error} />}
      {isLoading ? (
        <PageSpinner />
      ) : projects?.length === 0 ? (
        <div className="rounded-lg border border-line bg-surface">
          <EmptyState
            icon={<FolderPlus className="size-8" />}
            title="Create your first project"
            body="Projects hold issues, boards and workflows. Start from a template — Scrum, Kanban or Bug tracking — and adapt it later."
            action={
              <Button variant="primary" onClick={() => setCreating(true)}>
                New project
              </Button>
            }
          />
        </div>
      ) : (
        <div className="rounded-lg border border-line bg-surface">
          <div className="border-b border-line p-3">
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter projects" aria-label="Filter projects" className="max-w-xs" />
          </div>
          <div className="overflow-x-auto">
            <table className={table}>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Key</th>
                  <th>Template</th>
                  <th>Access</th>
                  <th>Lead</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((p) => (
                  <tr key={p.id} className="hover:bg-surface-2">
                    <td>
                      <Link to="/projects/$projectKey/board" params={{ projectKey: p.key }} className="flex items-center gap-2.5 font-medium hover:text-accent">
                        <ProjectAvatar projectKey={p.key} size={24} />
                        {p.name}
                      </Link>
                    </td>
                    <td className="font-mono text-xs text-muted">{p.key}</td>
                    <td className="text-muted capitalize">{p.template.replace("_", " ")}</td>
                    <td>
                      {p.access === "private" ? (
                        <Badge>
                          <Lock className="size-3" /> Private
                        </Badge>
                      ) : (
                        <Badge tone="accent">
                          <Globe className="size-3" /> Open
                        </Badge>
                      )}
                    </td>
                    <td>{p.lead ? <span className="inline-flex items-center gap-2"><UserAvatar user={p.lead} size={20} />{p.lead.displayName}</span> : <span className="text-faint">—</span>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      <CreateProjectDialog open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}

function suggestKey(name: string) {
  const words = name.toUpperCase().replace(/[^A-Z0-9 ]/g, "").split(/\s+/).filter(Boolean);
  if (words.length === 0) return "";
  const key = words.length === 1 ? words[0].slice(0, 4) : words.map((w) => w[0]).join("").slice(0, 5);
  return /^[A-Z]/.test(key) ? key : `P${key}`.slice(0, 5);
}

function CreateProjectDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: templates = [] } = useQuery({
    queryKey: ["project-templates"],
    queryFn: () => unwrap(api.GET("/project-templates")),
    enabled: open,
    staleTime: Infinity,
  });
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [template, setTemplate] = useState("scrum");
  const [access, setAccess] = useState<"open" | "private">("open");
  const [description, setDescription] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/projects", { body: { name, key, template, access, description } })),
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      onClose();
      navigate({ to: "/projects/$projectKey/board", params: { projectKey: p.key } });
    },
  });
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New project"
      size="lg"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="create-project" loading={create.isPending} disabled={!name || !key}>
            Create project
          </Button>
        </>
      }
    >
      <form
        id="create-project"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
        className="flex flex-col gap-4"
      >
        <div className="grid gap-4 sm:grid-cols-[1fr_9rem]">
          <Field label="Name" required error={fe.name}>
            {(id) => (
              <Input
                id={id}
                autoFocus
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  if (!keyTouched) setKey(suggestKey(e.target.value));
                }}
                placeholder="Checkout platform"
              />
            )}
          </Field>
          <Field label="Key" required error={fe.key} hint="Prefix for issue keys">
            {(id) => (
              <Input
                id={id}
                value={key}
                maxLength={10}
                onChange={(e) => {
                  setKeyTouched(true);
                  setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ""));
                }}
                className="font-mono uppercase"
                placeholder="CHK"
              />
            )}
          </Field>
        </div>
        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium">Template</legend>
          <div className="grid gap-2 sm:grid-cols-3">
            {templates.map((t) => (
              <button
                type="button"
                key={t.id}
                onClick={() => setTemplate(t.id)}
                aria-pressed={template === t.id}
                className={cn("rounded-md border p-3 text-left transition-colors", template === t.id ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
              >
                <p className="text-sm font-medium">{t.name}</p>
                <p className="mt-1 text-xs text-muted">{t.description}</p>
                <p className="mt-2 text-[11px] text-faint">{t.issueTypes.map((it) => it.name).join(" · ")}</p>
              </button>
            ))}
          </div>
        </fieldset>
        <Field label="Access" hint={access === "open" ? "Everyone in the organisation can browse and work on issues." : "Only members you add can see this project."}>
          {(id) => (
            <Select id={id} value={access} onChange={(e) => setAccess(e.target.value as "open" | "private")} className="max-w-56">
              <option value="open">Open to the organisation</option>
              <option value="private">Private</option>
            </Select>
          )}
        </Field>
        <Field label="Description">{(id) => <Textarea id={id} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />}</Field>
        {create.error && Object.keys(fe).length === 0 && <ErrorBox error={create.error} />}
      </form>
    </Modal>
  );
}
