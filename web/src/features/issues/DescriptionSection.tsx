import { useState } from "react";
import { Pencil } from "lucide-react";
import { Button, IconButton } from "@/components/ui";
import { Markdown } from "@/components/Markdown";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { useEscapeLayer } from "@/lib/escape";
import { Section, useIssue } from "./issue-context";

/** DescriptionSection renders the Markdown description; clicking it (or the pencil) edits it. */
export function DescriptionSection() {
  const { issue, update } = useIssue();
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const editable = issue.permissions.edit;
  // Escape abandons an untouched edit; changed text is kept until saved or cancelled.
  useEscapeLayer(draft !== null, () => draft?.trim() === issue.description.trim() && setDraft(null));

  const save = async () => {
    if (draft === null) return;
    if (draft.trim() === issue.description.trim()) {
      setDraft(null);
      return;
    }
    setSaving(true);
    const res = await update({ description: draft }, { description: draft });
    setSaving(false);
    if (res) setDraft(null);
  };

  return (
    <Section
      title="Description"
      actions={
        editable &&
        draft === null &&
        issue.description && (
          <IconButton label="Edit description" size="sm" onClick={() => setDraft(issue.description)}>
            <Pencil className="size-3.5" />
          </IconButton>
        )
      }
    >
      {draft !== null ? (
        <div>
          <MarkdownEditor value={draft} onChange={setDraft} autoFocus onSubmit={save} minHeight={140} placeholder="Describe the work, steps to reproduce, acceptance criteria…" />
          <div className="mt-2 flex items-center gap-2">
            <Button size="sm" variant="primary" onClick={save} loading={saving}>
              Save
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDraft(null)} disabled={saving}>
              Cancel
            </Button>
            <span className="ml-auto text-xs text-faint">⌘/Ctrl + Enter to save · Esc to cancel</span>
          </div>
        </div>
      ) : issue.description ? (
        <div
          className={editable ? "-mx-2 cursor-text rounded-md px-2 py-1 hover:bg-surface-2/60" : undefined}
          onClick={(e) => {
            // Links and selections inside the text keep working.
            if (!editable || (e.target as HTMLElement).closest("a, button") || window.getSelection()?.toString()) return;
            setDraft(issue.description);
          }}
        >
          <Markdown>{issue.description}</Markdown>
        </div>
      ) : editable ? (
        <button
          type="button"
          onClick={() => setDraft("")}
          className="w-full rounded-md border border-dashed border-line-strong px-3 py-3 text-left text-sm text-faint hover:border-accent hover:text-muted"
        >
          Add a description…
        </button>
      ) : (
        <p className="text-sm text-faint">No description.</p>
      )}
    </Section>
  );
}
