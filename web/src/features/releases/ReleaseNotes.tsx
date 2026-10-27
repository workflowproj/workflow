import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Download, Sparkles } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { Badge, Button, Textarea, useConfirm, useToast } from "@/components/ui";
import { Markdown } from "@/components/Markdown";
import { Segmented } from "@/features/reports/ChartBits";
import { CopyButton, Switch } from "@/features/settings/common";
import { notesFileName, type ReleaseDetail } from "./queries";

/** downloadText saves text as a file. */
function downloadText(name: string, text: string, type = "text/markdown") {
  const url = URL.createObjectURL(new Blob([text], { type: `${type};charset=utf-8` }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/**
 * ReleaseNotes edits a release's notes (Markdown): generate them from the done issues, edit,
 * save, copy or download them, and publish them to the help centres' What's new pages.
 */
export function ReleaseNotes({ release }: { release: ReleaseDetail }) {
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const manage = release.canManage;
  const [draft, setDraft] = useState(release.notes);
  const [tab, setTab] = useState<"write" | "preview">(manage ? "write" : "preview");
  // Pick up saved notes (another tab, a live update) unless there are unsaved edits.
  const [base, setBase] = useState(release.notes);
  useEffect(() => {
    if (release.notes !== base) {
      if (draft === base) setDraft(release.notes);
      setBase(release.notes);
    }
  }, [release.notes, base, draft]);
  const dirty = draft.trimEnd() !== release.notes;

  const patch = useMutation({
    mutationFn: (body: { notes?: string; notesPublished?: boolean }) => unwrap(api.PATCH("/releases/{releaseId}", { params: { path: { releaseId: release.id } }, body })),
    onSuccess: (_, body) => {
      qc.invalidateQueries({ queryKey: ["releases"] });
      if (body.notesPublished === true) toast.success("Release notes published to the help centres");
      else if (body.notesPublished === false) toast.success("Release notes taken off the help centres");
      else toast.success("Release notes saved");
    },
    onError: toast.error,
  });
  const generate = useMutation({
    mutationFn: () => unwrap(api.GET("/releases/{releaseId}/notes/generated", { params: { path: { releaseId: release.id } } })),
    onSuccess: async (g) => {
      if (g.issueCount === 0) {
        toast.info("No done issues to write notes from yet");
        return;
      }
      if (draft.trim() === g.notes.trim()) {
        toast.info("The notes already match the done issues");
        return;
      }
      if (
        draft.trim() &&
        !(await confirm({
          title: "Replace the release notes?",
          body: `The notes will be replaced by ones generated from the release's ${g.issueCount} done issue${g.issueCount === 1 ? "" : "s"}. Your edits will be lost unless you copy them first.`,
          confirmLabel: "Replace notes",
        }))
      )
        return;
      setDraft(g.notes);
      setTab("write");
    },
    onError: toast.error,
  });

  return (
    <div className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Segmented
          label="Release notes view"
          value={tab}
          onChange={setTab}
          options={
            manage
              ? [
                  { value: "write", label: "Write" },
                  { value: "preview", label: "Preview" },
                ]
              : [{ value: "preview", label: "Preview" }]
          }
        />
        <div className="flex flex-wrap items-center gap-1.5">
          {manage && (
            <Button size="sm" icon={<Sparkles className="size-3.5" />} loading={generate.isPending} onClick={() => generate.mutate()}>
              Generate
            </Button>
          )}
          <CopyButton value={draft} label="Copy" />
          <Button size="sm" icon={<Download className="size-3.5" />} disabled={!draft.trim()} onClick={() => downloadText(notesFileName(release.name), draft.trimEnd() + "\n")}>
            Download .md
          </Button>
        </div>
      </div>
      {tab === "write" && manage ? (
        <Textarea
          aria-label="Release notes (Markdown)"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={Math.min(24, Math.max(8, draft.split("\n").length + 1))}
          className="font-mono text-[13px] leading-relaxed"
          placeholder={"## Features\n\n- What changed, for the people who use it\n\nGenerate fills this from the release's done issues."}
        />
      ) : draft.trim() ? (
        <div className="min-h-24 rounded-md border border-line px-4 py-3">
          <Markdown>{draft}</Markdown>
        </div>
      ) : (
        <p className="rounded-md border border-dashed border-line px-4 py-6 text-center text-sm text-muted">
          {manage ? "No release notes yet. Generate them from the done issues, or write your own." : "No release notes yet."}
        </p>
      )}
      {manage && (
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-3">
          <div className="flex flex-col gap-1">
            <Switch
              checked={release.notesPublished}
              disabled={patch.isPending || (!release.notesPublished && (!release.notes.trim() || dirty))}
              onChange={(v) => patch.mutate({ notesPublished: v })}
              label="Show on the help centres' What's new"
            />
            <p className="text-xs text-muted">
              {release.notesPublished
                ? "Customers of the service desks that chose this project read these notes in their help centre."
                : dirty || !release.notes.trim()
                  ? "Save the notes to publish them."
                  : "Service desks choose which projects' notes they show in Help centre settings."}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {release.notesPublished && <Badge tone="success">Published</Badge>}
            {dirty && (
              <Button size="sm" onClick={() => setDraft(release.notes)}>
                Discard changes
              </Button>
            )}
            <Button size="sm" variant="primary" disabled={!dirty} loading={patch.isPending && patch.variables?.notes !== undefined} onClick={() => patch.mutate({ notes: draft })}>
              Save notes
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
