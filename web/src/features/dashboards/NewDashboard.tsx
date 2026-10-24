import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, LayoutDashboard, Plus, Target, UserRound } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { projectsQuery } from "@/api/queries";
import { Button, Checkbox, Field, Input, Modal, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ProjectPicker } from "@/features/timeline/Toolbar";
import { cn } from "@/lib/cn";
import type { DashboardInput } from "./model";
import { TEMPLATES, templateById, type DashboardTemplate } from "./templates";

type TemplateId = DashboardTemplate["id"];

const ICONS: Record<TemplateId, React.ReactNode> = {
  sprint: <Target className="size-5" />,
  flow: <Activity className="size-5" />,
  mine: <UserRound className="size-5" />,
  blank: <Plus className="size-5" />,
};

/** TemplateCards shows the starting points; `selected` makes it a radio group. */
export function TemplateCards({ selected, onPick, className }: { selected?: TemplateId; onPick: (id: TemplateId) => void; className?: string }) {
  return (
    <div role={selected ? "radiogroup" : undefined} aria-label="Start from" className={cn("grid gap-3 sm:grid-cols-2", className)}>
      {TEMPLATES.map((t) => (
        <button
          key={t.id}
          type="button"
          role={selected ? "radio" : undefined}
          aria-checked={selected ? selected === t.id : undefined}
          onClick={() => onPick(t.id)}
          className={cn(
            "flex items-start gap-3 rounded-lg border bg-surface p-3.5 text-left transition-colors focus-visible:outline-2 focus-visible:outline-accent",
            selected === t.id ? "border-accent bg-accent-soft/50 ring-1 ring-accent" : "border-line hover:border-line-strong hover:bg-surface-2",
          )}
        >
          <span className={cn("mt-0.5 inline-flex size-9 shrink-0 items-center justify-center rounded-md", selected === t.id ? "bg-accent text-accent-ink" : "bg-surface-2 text-accent")}>{ICONS[t.id]}</span>
          <span className="min-w-0">
            <span className="block text-sm font-medium">{t.name}</span>
            <span className="mt-0.5 block text-[13px] text-muted">{t.description}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

/** useCreateDashboard posts a dashboard and opens it (in edit mode when it starts empty). */
export function useCreateDashboard() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: (body: DashboardInput) => unwrap(api.POST("/dashboards", { body })),
    onSuccess: (d) => {
      qc.setQueryData(["dashboards", d.id], d);
      qc.invalidateQueries({ queryKey: ["dashboards"], exact: true });
      navigate({ to: "/dashboards/$dashboardId", params: { dashboardId: d.id }, search: d.gadgets.length === 0 ? { edit: true } : {} });
    },
  });
}

/**
 * NewDashboardDialog creates a dashboard from a template (or blank), asking for the projects
 * the template needs: one for the sprint dashboard, any number (or all) for team flow.
 */
export function NewDashboardDialog({ open, onClose, template: initial = "sprint" }: { open: boolean; onClose: () => void; template?: TemplateId }) {
  const { data: projects = [] } = useQuery({ ...projectsQuery, enabled: open });
  const [templateId, setTemplateId] = useState<TemplateId>(initial);
  const [keys, setKeys] = useState<string[]>([]);
  const [name, setName] = useState<string | null>(null);
  const [shared, setShared] = useState<boolean | null>(null);
  const create = useCreateDashboard();
  const { reset } = create;
  useEffect(() => {
    if (!open) return;
    setTemplateId(initial);
    setName(null);
    setShared(null);
    reset();
  }, [open, initial, reset]);
  const t = templateById(templateId);
  // A sprint dashboard follows one project; default to the first the viewer can see.
  const projectKeys = t.projects === "one" ? [keys[0] ?? projects[0]?.key].filter(Boolean) : t.projects === "many" ? keys : [];
  const built = t.build(projectKeys);
  const needsProject = t.projects === "one" && projectKeys.length === 0;
  const fe = create.error instanceof ApiError ? create.error.fieldErrors() : {};
  const submit = () => create.mutate({ ...built, name: (name ?? built.name).trim() || built.name, shared: shared ?? built.shared });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="New dashboard"
      size="xl"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="new-dashboard" loading={create.isPending} disabled={needsProject}>
            Create dashboard
          </Button>
        </>
      }
    >
      <form
        id="new-dashboard"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        className="flex flex-col gap-5"
      >
        <TemplateCards selected={templateId} onPick={(id) => (setTemplateId(id), setName(null), setShared(null))} />
        {t.includes.length > 0 && (
          <p className="-mt-2 text-[13px] text-muted">
            <LayoutDashboard className="mr-1.5 inline size-3.5 align-[-2px]" />
            Includes {t.includes.join(", ").toLowerCase()}. You can change any of it afterwards.
          </p>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" required error={fe.name}>
            {(id) => <Input id={id} value={name ?? built.name} maxLength={80} onChange={(e) => setName(e.target.value)} />}
          </Field>
          {t.projects === "one" && (
            <Field label="Project" hint="The project whose sprints it follows." error={needsProject ? "Create a project first." : undefined}>
              {(id) => (
                <Select id={id} value={projectKeys[0] ?? ""} onChange={(e) => setKeys([e.target.value])}>
                  {projects.map((p) => (
                    <option key={p.id} value={p.key}>
                      {p.name} ({p.key})
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
          {t.projects === "many" && (
            <div role="group" aria-label="Projects" className="flex flex-col gap-1.5">
              <p className="text-[13px] font-medium">Projects</p>
              <ProjectPicker value={keys} onChange={setKeys} />
              <p className="text-xs text-muted">None picked means every project each viewer can browse.</p>
            </div>
          )}
        </div>
        <Checkbox checked={shared ?? built.shared ?? false} onChange={(e) => setShared(e.target.checked)} label="Share with everyone in the organisation (each person still sees only issues they can browse)" />
        {create.error && !fe.name ? <ErrorBox error={create.error} /> : null}
      </form>
    </Modal>
  );
}
