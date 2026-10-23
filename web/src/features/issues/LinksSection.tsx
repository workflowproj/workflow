import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import type { IssueDetail } from "@/api/types";
import { Button, IconButton, Select, useToast } from "@/components/ui";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import { IssueKeyLink, Section, useIssue, useIssueAction } from "./issue-context";
import { IssuePicker } from "./IssuePicker";

type LinkType = Schemas["LinkType"];
type Direction = Schemas["LinkDirection"];

const LINK_OPTIONS: { id: string; label: string; type: LinkType; direction: Direction }[] = [
  { id: "blocks", label: "blocks", type: "blocks", direction: "outward" },
  { id: "blocked-by", label: "is blocked by", type: "blocks", direction: "inward" },
  { id: "relates", label: "relates to", type: "relates", direction: "outward" },
  { id: "duplicates", label: "duplicates", type: "duplicates", direction: "outward" },
  { id: "duplicated-by", label: "is duplicated by", type: "duplicates", direction: "inward" },
  { id: "clones", label: "clones", type: "clones", direction: "outward" },
  { id: "cloned-by", label: "is cloned by", type: "clones", direction: "inward" },
];
const ORDER = LINK_OPTIONS.map((o) => o.label);

/** LinksSection groups issue links by label ("blocks", "is blocked by", …) and adds or removes them. */
export function LinksSection() {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const editable = issue.permissions.edit;
  useIssueAction("add-link", () => setAdding(true));
  useEffect(() => {
    if (adding) ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [adding]);

  const remove = useMutation({
    mutationFn: (link: Schemas["IssueLink"]) => unwrap(api.DELETE("/issue-links/{linkId}", { params: { path: { linkId: link.id } } })),
    onMutate: (link) => {
      const prev = qc.getQueryData<IssueDetail>(["issue", issue.key]);
      if (prev) qc.setQueryData<IssueDetail>(["issue", issue.key], { ...prev, links: prev.links.filter((l) => l.id !== link.id) });
      return { prev };
    },
    onError: (err, _l, ctx) => {
      if (ctx?.prev) qc.setQueryData(["issue", issue.key], ctx.prev);
      toast.error(err);
    },
    onSettled: (_d, _e, link) => {
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: ["issue", link.issue.key] });
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
    },
  });

  const groups = new Map<string, Schemas["IssueLink"][]>();
  for (const l of [...issue.links].sort((a, b) => rank(a.label) - rank(b.label))) {
    groups.set(l.label, [...(groups.get(l.label) ?? []), l]);
  }
  if (issue.links.length === 0 && !adding) return null;

  return (
    <div ref={ref} className="min-w-0 scroll-mt-16">
      <Section
        title="Linked issues"
        count={issue.links.length}
        actions={
          editable &&
          !adding && (
            <IconButton label="Link issue" size="sm" onClick={() => setAdding(true)}>
              <Plus className="size-4" />
            </IconButton>
          )
        }
      >
        {groups.size > 0 && (
          <div className="flex flex-col gap-3">
            {[...groups].map(([label, links]) => (
              <div key={label}>
                <p className="mb-1 text-xs font-medium text-muted">{label}</p>
                <ul className="overflow-hidden rounded-md border border-line">
                  {links.map((l) => (
                    <li key={l.id} className="group flex items-center gap-2.5 border-b border-line px-3 py-2 text-sm last:border-b-0 hover:bg-surface-2">
                      <IssueTypeIcon type={l.issue.type} />
                      <IssueKeyLink issueKey={l.issue.key} className={cn("shrink-0 font-mono text-xs text-muted", l.issue.status.category === "done" && "line-through")} />
                      <IssueKeyLink issueKey={l.issue.key} className="min-w-0 flex-1 truncate text-ink">
                        {l.issue.summary}
                      </IssueKeyLink>
                      <StatusBadge status={l.issue.status} className="shrink-0" />
                      {editable && (
                        <IconButton
                          label={`Remove link to ${l.issue.key}`}
                          size="sm"
                          className="size-6 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                          onClick={() => remove.mutate(l)}
                        >
                          <X className="size-3.5" />
                        </IconButton>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
        {adding && <AddLink onDone={() => setAdding(false)} />}
      </Section>
    </div>
  );
}

const rank = (label: string) => {
  const i = ORDER.indexOf(label);
  return i < 0 ? ORDER.length : i;
};

function AddLink({ onDone }: { onDone: () => void }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const [kind, setKind] = useState("relates");
  const option = LINK_OPTIONS.find((o) => o.id === kind)!;
  const create = useMutation({
    mutationFn: (targetKey: string) =>
      unwrap(api.POST("/issues/{issueKey}/links", { params: { path: { issueKey: issue.key } }, body: { type: option.type, direction: option.direction, targetKey } })),
    onSuccess: (link) => {
      toast.success(`${issue.key} ${link.label} ${link.issue.key}`);
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: ["issue", link.issue.key] });
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
      onDone();
    },
    onError: (err) => toast.error(err),
  });
  const linked = issue.links.filter((l) => l.type === option.type && l.direction === option.direction).map((l) => l.issue.key);
  return (
    <div className="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-line bg-surface-2/60 p-2 sm:flex-nowrap">
      <Select aria-label="Link type" value={kind} onChange={(e) => setKind(e.target.value)} className="w-40 shrink-0">
        {LINK_OPTIONS.map((o) => (
          <option key={o.id} value={o.id}>
            {o.label}
          </option>
        ))}
      </Select>
      <IssuePicker autoFocus exclude={[issue.key, ...linked]} onPick={(i) => create.mutate(i.key)} onCancel={onDone} />
      <Button size="md" variant="ghost" onClick={onDone} loading={create.isPending}>
        Cancel
      </Button>
    </div>
  );
}
