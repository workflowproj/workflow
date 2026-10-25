import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, unwrap } from "@/api/client";
import { Button, Field, Input, Modal, Select, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { todayISO } from "@/features/backlog/dates";
import type { Release } from "./queries";

function useInvalidateReleases(projectKey: string) {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["releases", projectKey] });
    qc.invalidateQueries({ queryKey: ["search"] });
  };
}

/** ReleaseFormDialog creates a release (release = null) or edits one. */
export function ReleaseFormDialog({ open, onClose, release, projectKey }: { open: boolean; onClose: () => void; release: Release | null; projectKey: string }) {
  const invalidate = useInvalidateReleases(projectKey);
  const toast = useToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [start, setStart] = useState("");
  const [date, setDate] = useState("");
  useEffect(() => {
    if (!open) return;
    setName(release?.name ?? "");
    setDescription(release?.description ?? "");
    setStart(release?.startDate ?? "");
    setDate(release?.releaseDate ?? "");
  }, [open, release]);
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), description, startDate: start || null, releaseDate: date || null };
      return release
        ? unwrap(api.PATCH("/releases/{releaseId}", { params: { path: { releaseId: release.id } }, body }))
        : unwrap(api.POST("/projects/{projectKey}/releases", { params: { path: { projectKey } }, body }));
    },
    onSuccess: (r) => {
      invalidate();
      toast.success(release ? `${r.name} saved` : `${r.name} created`);
      onClose();
    },
  });
  const fe = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const duplicate = save.error instanceof ApiError && save.error.code === "duplicate_release";
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={release ? `Edit ${release.name}` : "Create release"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="release-form" loading={save.isPending} disabled={!name.trim()}>
            {release ? "Save" : "Create release"}
          </Button>
        </>
      }
    >
      <form
        id="release-form"
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name" required error={fe.name ?? (duplicate ? save.error?.message : undefined)} hint="For example 2026.11 or v2.4.0">
          {(id) => <Input id={id} autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={80} invalid={!!fe.name || duplicate} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Start date" error={fe.startDate}>
            {(id) => <Input id={id} type="date" value={start} onChange={(e) => setStart(e.target.value)} />}
          </Field>
          <Field label="Release date" error={fe.releaseDate}>
            {(id) => <Input id={id} type="date" value={date} min={start || undefined} onChange={(e) => setDate(e.target.value)} />}
          </Field>
        </div>
        <Field label="Description" error={fe.description}>
          {(id) => <Textarea id={id} rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />}
        </Field>
        {save.error && !duplicate && Object.keys(fe).length === 0 && <ErrorBox error={save.error} />}
      </form>
    </Modal>
  );
}

/** ShipReleaseDialog marks a release released; unfinished issues stay or move to another release. */
export function ShipReleaseDialog({
  open,
  onClose,
  release,
  others,
  openCount,
  projectKey,
}: {
  open: boolean;
  onClose: () => void;
  release: Release | null;
  others: Release[];
  openCount: number;
  projectKey: string;
}) {
  const invalidate = useInvalidateReleases(projectKey);
  const toast = useToast();
  const [target, setTarget] = useState("");
  const [date, setDate] = useState("");
  useEffect(() => {
    if (!open) return;
    setTarget(others[0]?.id ?? "");
    setDate(release?.releaseDate && release.releaseDate <= todayISO() ? release.releaseDate : todayISO());
    // Reset only when the dialog opens, not when the release list refreshes.
  }, [open]);
  const ship = useMutation({
    mutationFn: async () => {
      const releaseId = release!.id;
      if (date && date !== release!.releaseDate) await unwrap(api.PATCH("/releases/{releaseId}", { params: { path: { releaseId } }, body: { releaseDate: date } }));
      return unwrap(api.POST("/releases/{releaseId}/release", { params: { path: { releaseId } }, body: { moveOpenIssuesTo: openCount > 0 && target ? target : undefined } }));
    },
    onSuccess: (r) => {
      invalidate();
      toast.success(`${r.name} released`);
      onClose();
    },
  });
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={release ? `Release ${release.name}` : "Release"}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" loading={ship.isPending} onClick={() => ship.mutate()}>
            Release
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Release date">{(id) => <Input id={id} type="date" value={date} onChange={(e) => setDate(e.target.value)} className="max-w-48" />}</Field>
        {openCount > 0 ? (
          <Field
            label={`${openCount} unfinished issue${openCount === 1 ? "" : "s"}`}
            hint={target ? "They'll get the release you pick as their fix version instead." : "They'll no longer have a fix version."}
          >
            {(id) => (
              <Select id={id} value={target} onChange={(e) => setTarget(e.target.value)}>
                <option value="">Remove them from this release</option>
                {others.map((r) => (
                  <option key={r.id} value={r.id}>
                    Move them to {r.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        ) : (
          <p className="text-sm text-muted">Every issue in this release is done.</p>
        )}
        {ship.error && <ErrorBox error={ship.error} />}
      </div>
    </Modal>
  );
}
