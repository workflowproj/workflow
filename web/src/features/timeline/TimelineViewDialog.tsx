import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import { projectsQuery } from "@/api/queries";
import { Button, Checkbox, Field, Input, Modal, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { ProjectAvatar } from "@/layout/AppShell";
import { isProgramme } from "@/lib/project";
import type { TimelineView } from "./views";

/**
 * TimelineViewDialog saves a group of projects as a timeline, or (with `view`) changes one: its
 * name, description, projects and sharing. Programmes aren't offered: each has its own timeline.
 */
export function TimelineViewDialog({
  open,
  onClose,
  view,
  initialProjects = [],
  onSaved,
}: {
  open: boolean;
  onClose: () => void;
  view?: TimelineView;
  /** Project keys to start a new timeline with (the picker's current choice). */
  initialProjects?: string[];
  onSaved?: (view: TimelineView) => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: all = [] } = useQuery(projectsQuery);
  const projects = useMemo(() => all.filter((p) => !isProgramme(p)).sort((a, b) => a.name.localeCompare(b.name)), [all]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [shared, setShared] = useState(false);
  const [chosen, setChosen] = useState<string[]>([]);

  // Start from the timeline's values (or the picker's choice) each time the dialog opens.
  useEffect(() => {
    if (!open) return;
    setName(view?.name ?? "");
    setDescription(view?.description ?? "");
    setShared(view?.shared ?? false);
    setChosen(view ? view.projects.map((p) => p.key) : initialProjects);
    save.reset();
  }, [open, view?.id]);

  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), description: description.trim(), shared, projects: chosen };
      return view
        ? unwrap(api.PUT("/timeline-views/{viewId}", { params: { path: { viewId: view.id } }, body }))
        : unwrap(api.POST("/timeline-views", { body }));
    },
    onSuccess: (saved) => {
      qc.invalidateQueries({ queryKey: ["timeline-views"] });
      toast.success(view ? `Saved "${saved.name}"` : `Timeline "${saved.name}" created`);
      onSaved?.(saved);
      onClose();
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const toggle = (key: string) => setChosen((c) => (c.includes(key) ? c.filter((k) => k !== key) : [...c, key]));

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={view ? "Edit timeline" : "New timeline"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="timeline-view" loading={save.isPending} disabled={!name.trim() || chosen.length === 0}>
            {view ? "Save" : "Create timeline"}
          </Button>
        </>
      }
    >
      <form
        id="timeline-view"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name" required error={fe.name}>
          {(id) => <Input id={id} autoFocus value={name} maxLength={100} onChange={(e) => setName(e.target.value)} placeholder="e.g. Payments teams" />}
        </Field>
        <Field label="Description" error={fe.description}>
          {(id) => <Textarea id={id} rows={2} maxLength={1000} value={description} onChange={(e) => setDescription(e.target.value)} />}
        </Field>
        <fieldset>
          <legend className="mb-1.5 text-[13px] font-medium">
            Projects <span className="font-normal text-muted">({chosen.length} chosen)</span>
          </legend>
          <div className="flex max-h-56 flex-col gap-1.5 overflow-y-auto rounded-md border border-line p-2.5">
            {projects.length === 0 && <p className="text-[13px] text-muted">No projects to choose from.</p>}
            {projects.map((p) => (
              <Checkbox
                key={p.id}
                checked={chosen.includes(p.key)}
                onChange={() => toggle(p.key)}
                label={
                  <span className="flex items-center gap-2">
                    <ProjectAvatar projectKey={p.key} size={18} />
                    {p.name} <span className="font-mono text-[11px] text-faint">{p.key}</span>
                  </span>
                }
              />
            ))}
          </div>
          {fe.projects && <p className="mt-1 text-xs text-danger">{fe.projects}</p>}
          {view && view.hiddenProjects > 0 && (
            <p className="mt-1.5 text-xs text-muted">
              {view.hiddenProjects === 1 ? "1 project you can't browse stays" : `${view.hiddenProjects} projects you can't browse stay`} in this timeline.
            </p>
          )}
        </fieldset>
        <Checkbox
          label="Share with everyone in the organisation (each person sees only the projects they can browse)"
          checked={shared}
          onChange={(e) => setShared(e.target.checked)}
        />
        {save.error && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}
