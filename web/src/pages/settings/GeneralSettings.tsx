import { useState } from "react";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, Lock, Trash2 } from "lucide-react";
import { api, ApiError, unwrap, type Schemas } from "@/api/client";
import { useProject } from "@/api/queries";
import type { User } from "@/api/types";
import { Badge, Button, Field, Input, Modal, PageHeader, PageSpinner, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserPicker } from "@/components/UserPicker";
import { Callout, SettingsSection } from "@/features/settings/common";
import { rolesQuery } from "@/features/settings/queries";
import { cn } from "@/lib/cn";

const route = getRouteApi("/app/projects/$projectKey/settings/");

type Project = Schemas["Project"];

export function GeneralSettings() {
  const { projectKey } = route.useParams();
  const { data: project, isLoading, error } = useProject(projectKey);
  if (isLoading) return <PageSpinner />;
  if (error || !project) return <ErrorBox error={error ?? new Error("Project not found")} />;
  return <GeneralForm key={project.id + project.key} project={project} />;
}

function GeneralForm({ project }: { project: Project }) {
  const qc = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();
  const { data: roles = [] } = useQuery(rolesQuery);
  const [name, setName] = useState(project.name);
  const [key, setKey] = useState(project.key);
  const [description, setDescription] = useState(project.description);
  const [lead, setLead] = useState<User | null>(project.lead ?? null);
  const [access, setAccess] = useState(project.access);
  const [openRole, setOpenRole] = useState(project.openRoleKey ?? "member");
  const [deleting, setDeleting] = useState(false);

  const patch: Schemas["ProjectPatch"] = {};
  if (name.trim() !== project.name) patch.name = name.trim();
  if (key !== project.key) patch.key = key;
  if (description !== project.description) patch.description = description;
  if ((lead?.id ?? null) !== (project.lead?.id ?? null)) patch.leadId = lead?.id ?? null;
  if (access !== project.access) patch.access = access;
  if (access === "open" && openRole !== (project.openRoleKey ?? "member")) patch.openRoleKey = openRole;
  const dirty = Object.keys(patch).length > 0;

  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH("/projects/{projectKey}", { params: { path: { projectKey: project.key } }, body: patch })),
    onSuccess: (p) => {
      qc.setQueryData(["project", p.key], p);
      qc.invalidateQueries({ queryKey: ["projects"] });
      if (p.key !== project.key) {
        qc.removeQueries({ queryKey: ["project", project.key] });
        toast.success(`Project key changed to ${p.key}. ${project.key} keeps working as an alias.`);
        navigate({ to: "/projects/$projectKey/settings", params: { projectKey: p.key }, replace: true });
      } else {
        qc.invalidateQueries({ queryKey: ["project", p.key] });
        toast.success("Project settings saved");
      }
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const keyError = fe.key ?? (save.error instanceof ApiError && save.error.code === "duplicate_key" ? save.error.message : undefined);
  const aliases = project.keyAliases ?? [];

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <PageHeader title="General" subtitle="Name, key, lead and who can access the project." />
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (dirty) save.mutate();
        }}
        className="flex flex-col gap-6"
      >
        <SettingsSection title="Details">
          <div className="grid gap-4 p-4 sm:grid-cols-[1fr_10rem]">
            <Field label="Name" required error={fe.name}>
              {(id) => <Input id={id} value={name} maxLength={120} onChange={(e) => setName(e.target.value)} invalid={!!fe.name} />}
            </Field>
            <Field label="Key" required error={keyError}>
              {(id) => (
                <Input id={id} value={key} maxLength={10} className="font-mono uppercase" invalid={!!keyError} onChange={(e) => setKey(e.target.value.toUpperCase().replace(/[^A-Z0-9]/g, ""))} />
              )}
            </Field>
            {key !== project.key && (
              <Callout tone="warning" className="sm:col-span-2" title={`Issue keys will change from ${project.key}-123 to ${key || "…"}-123`}>
                The old key stays as an alias, so existing links, bookmarks, branch names and commit messages that mention {project.key}-123 keep resolving. New issues use the new key.
              </Callout>
            )}
            {aliases.length > 0 && (
              <p className="text-xs text-muted sm:col-span-2">
                Also answers to{" "}
                {aliases.map((a) => (
                  <Badge key={a} className="mr-1 font-mono">
                    {a}
                  </Badge>
                ))}
                (previous keys)
              </p>
            )}
            <Field label="Description" className="sm:col-span-2">
              {(id) => <Textarea id={id} rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />}
            </Field>
            <Field label="Project lead" hint="Shown on the project and a sensible default contact for questions." className="sm:col-span-2">
              {() => (
                <div className="max-w-xs">
                  <UserPicker value={lead} onChange={setLead} project={project.key} noneLabel="No lead" label="Project lead" />
                </div>
              )}
            </Field>
          </div>
        </SettingsSection>

        <SettingsSection title="Access" description="Who can see the project without being added as a member.">
          <div className="flex flex-col gap-4 p-4">
            <div role="radiogroup" aria-label="Access" className="grid gap-2 sm:grid-cols-2">
              {(
                [
                  ["open", Globe, "Open to the organisation", "Every organisation member (not guests) gets the open role below. Explicit members get their own role instead."],
                  ["private", Lock, "Private", "Only members, directly or through a group, and organisation admins can see the project."],
                ] as const
              ).map(([value, Icon, title, body]) => (
                <button
                  key={value}
                  type="button"
                  role="radio"
                  aria-checked={access === value}
                  onClick={() => setAccess(value)}
                  className={cn("flex gap-3 rounded-md border p-3 text-left transition-colors", access === value ? "border-accent bg-accent-soft" : "border-line hover:border-line-strong")}
                >
                  <Icon className={cn("mt-0.5 size-4 shrink-0", access === value ? "text-accent" : "text-muted")} />
                  <span>
                    <span className="block text-sm font-medium">{title}</span>
                    <span className="mt-0.5 block text-xs text-muted">{body}</span>
                  </span>
                </button>
              ))}
            </div>
            {access === "open" && (
              <Field label="Open role" error={fe.openRoleKey} hint="The project role organisation members get unless they are explicit members (directly or via a group).">
                {(id) => (
                  <Select id={id} value={openRole} onChange={(e) => setOpenRole(e.target.value)} className="max-w-xs">
                    {roles.map((r) => (
                      <option key={r.key} value={r.key}>
                        {r.name}
                      </option>
                    ))}
                  </Select>
                )}
              </Field>
            )}
          </div>
        </SettingsSection>

        {save.error && Object.keys(fe).length === 0 && !keyError && <ErrorBox error={save.error} />}
        <div className="flex items-center gap-2">
          <Button type="submit" variant="primary" disabled={!dirty} loading={save.isPending}>
            Save changes
          </Button>
          {dirty && (
            <Button
              variant="ghost"
              onClick={() => {
                setName(project.name);
                setKey(project.key);
                setDescription(project.description);
                setLead(project.lead ?? null);
                setAccess(project.access);
                setOpenRole(project.openRoleKey ?? "member");
                save.reset();
              }}
            >
              Reset
            </Button>
          )}
        </div>
      </form>

      <SettingsSection title={<span className="text-danger">Danger zone</span>} className="border-danger/30">
        <div className="flex flex-wrap items-center justify-between gap-3 p-4">
          <div>
            <p className="text-sm font-medium">Delete this project</p>
            <p className="text-[13px] text-muted">Deletes its issues, boards, sprints, releases, workflows and automation. This cannot be undone.</p>
          </div>
          <Button variant="danger" icon={<Trash2 className="size-4" />} onClick={() => setDeleting(true)}>
            Delete project
          </Button>
        </div>
      </SettingsSection>
      <DeleteProjectDialog open={deleting} onClose={() => setDeleting(false)} project={project} />
    </div>
  );
}

function DeleteProjectDialog({ open, onClose, project }: { open: boolean; onClose: () => void; project: Project }) {
  const [typed, setTyped] = useState("");
  const qc = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE("/projects/{projectKey}", { params: { path: { projectKey: project.key } } })),
    onSuccess: () => {
      qc.removeQueries({ queryKey: ["project", project.key] });
      qc.invalidateQueries({ queryKey: ["projects"] });
      toast.success(`Deleted ${project.name}`);
      navigate({ to: "/projects" });
    },
  });
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Delete ${project.name}?`}
      size="sm"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="danger" disabled={typed !== project.key} loading={del.isPending} onClick={() => del.mutate()}>
            Delete project
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <p className="text-muted">All of the project's issues and configuration are permanently deleted.</p>
        <Field label={`Type ${project.key} to confirm`}>
          {(id) => <Input id={id} value={typed} onChange={(e) => setTyped(e.target.value.toUpperCase())} className="font-mono" autoComplete="off" />}
        </Field>
        {del.error && <ErrorBox error={del.error} />}
      </div>
    </Modal>
  );
}
