import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { issueTypesQuery } from "@/api/queries";
import { Button, IconButton, useToast } from "@/components/ui";
import { IssueTypeIcon } from "@/components/issue-bits";

/**
 * InlineCreate is the "+ Create issue" row under a sprint or the backlog: pick a standard
 * issue type, type a summary, Enter creates it in place (and keeps the row open for the next).
 */
export function InlineCreate({ projectKey, sprintId }: { projectKey: string; sprintId?: string }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: types = [] } = useQuery(issueTypesQuery(projectKey));
  const standard = types.filter((t) => t.level === 0);
  const [open, setOpen] = useState(false);
  const [typeKey, setTypeKey] = useState("");
  const [summary, setSummary] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const type = standard.find((t) => t.key === typeKey) ?? standard.find((t) => t.key === "story") ?? standard[0];

  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/issues", { body: { projectKey, typeKey: type!.key, summary: summary.trim(), sprintId } })),
    onSuccess: (issue) => {
      setSummary("");
      for (const k of ["backlog", "board", "search"]) qc.invalidateQueries({ queryKey: [k] });
      toast.success(`${issue.key} created`);
      input.current?.focus();
    },
    onError: (e) => toast.error(e),
  });

  if (!open)
    return (
      <button type="button" onClick={() => setOpen(true)} className="flex w-full items-center gap-2 rounded-b-lg px-3 py-2 text-left text-[13px] text-muted hover:bg-surface-2 hover:text-ink focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent">
        <Plus className="size-4" /> Create issue
      </button>
    );
  const close = () => {
    setOpen(false);
    setSummary("");
  };
  return (
    <form
      className="flex items-center gap-2 border-t border-line px-2 py-1.5"
      onSubmit={(e) => {
        e.preventDefault();
        if (summary.trim() && type) create.mutate();
      }}
      onKeyDown={(e) => e.key === "Escape" && close()}
    >
      <span className="relative">
        {type && (
          <span aria-hidden className="pointer-events-none absolute top-1/2 left-2 flex -translate-y-1/2">
            <IssueTypeIcon type={type} />
          </span>
        )}
        <select
          aria-label="Issue type"
          value={type?.key ?? ""}
          onChange={(e) => setTypeKey(e.target.value)}
          className="h-8 rounded-md border border-line-strong bg-surface pr-2 pl-8 text-[13px] focus:border-accent focus:outline-none"
        >
          {standard.map((t) => (
            <option key={t.id} value={t.key}>
              {t.name}
            </option>
          ))}
        </select>
      </span>
      <input
        ref={input}
        autoFocus
        value={summary}
        onChange={(e) => setSummary(e.target.value)}
        maxLength={255}
        placeholder="What needs to be done?"
        aria-label="Summary"
        className="h-8 min-w-0 flex-1 rounded-md border border-line-strong bg-surface px-2.5 text-sm placeholder:text-faint focus:border-accent focus:ring-2 focus:ring-accent/25 focus:outline-none"
      />
      <Button type="submit" size="sm" variant="primary" loading={create.isPending} disabled={!summary.trim() || !type}>
        Create
      </Button>
      <IconButton label="Cancel" size="sm" onClick={close}>
        <X className="size-4" />
      </IconButton>
    </form>
  );
}
