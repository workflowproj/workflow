import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { api, unwrap, type Schemas } from "@/api/client";
import type { IssueDetail } from "@/api/types";
import { Button, IconButton, Select, useToast } from "@/components/ui";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { cn } from "@/lib/cn";
import { useEscapeLayer } from "@/lib/escape";
import { IssueKeyLink, Section, useIssue, useIssueAction } from "./issue-context";
import { IssuePicker } from "./IssuePicker";

type LinkType = Schemas["LinkType"];
type Direction = Schemas["LinkDirection"];

type Kind = Schemas["IssueTypeKind"];

/**
 * The ways to link: `from` is the kind this issue must be (any when unset) and `scope` narrows
 * the picker to the kind at the other end (work mitigates a risk, implements a decision; a
 * decision supersedes a decision).
 */
const LINK_OPTIONS: { id: string; label: string; type: LinkType; direction: Direction; from?: Kind; scope?: string }[] = [
  { id: "blocks", label: "blocks", type: "blocks", direction: "outward" },
  { id: "blocked-by", label: "is blocked by", type: "blocks", direction: "inward" },
  { id: "relates", label: "relates to", type: "relates", direction: "outward" },
  { id: "duplicates", label: "duplicates", type: "duplicates", direction: "outward" },
  { id: "duplicated-by", label: "is duplicated by", type: "duplicates", direction: "inward" },
  { id: "clones", label: "clones", type: "clones", direction: "outward" },
  { id: "cloned-by", label: "is cloned by", type: "clones", direction: "inward" },
  { id: "mitigates", label: "mitigates", type: "mitigates", direction: "outward", from: "work", scope: "kind = risk" },
  { id: "mitigated-by", label: "is mitigated by", type: "mitigates", direction: "inward", from: "risk", scope: "kind = work" },
  { id: "implements", label: "implements", type: "implements", direction: "outward", from: "work", scope: "kind = decision" },
  { id: "implemented-by", label: "is implemented by", type: "implements", direction: "inward", from: "decision", scope: "kind = work" },
  { id: "supersedes", label: "supersedes", type: "supersedes", direction: "outward", from: "decision", scope: "kind = decision" },
  { id: "superseded-by", label: "is superseded by", type: "supersedes", direction: "inward", from: "decision", scope: "kind = decision" },
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

  // A risk lists its mitigating work in its own section.
  const links = issue.type.kind === "risk" ? issue.links.filter((l) => !(l.type === "mitigates" && l.direction === "inward")) : issue.links;
  const groups = new Map<string, Schemas["IssueLink"][]>();
  for (const l of [...links].sort((a, b) => rank(a.label) - rank(b.label))) {
    groups.set(l.label, [...(groups.get(l.label) ?? []), l]);
  }
  if (links.length === 0 && !adding) return null;

  return (
    <div ref={ref} className="min-w-0 scroll-mt-16">
      <Section
        title="Linked issues"
        count={links.length}
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
  const options = LINK_OPTIONS.filter((o) => !o.from || o.from === issue.type.kind);
  const [kind, setKind] = useState("relates");
  const option = options.find((o) => o.id === kind) ?? options[0];
  const create = useMutation({
    mutationFn: (targetKey: string) =>
      unwrap(api.POST("/issues/{issueKey}/links", { params: { path: { issueKey: issue.key } }, body: { type: option.type, direction: option.direction, targetKey } })),
    onSuccess: (link) => {
      toast.success(`${issue.key} ${link.label} ${link.issue.key}`);
      qc.invalidateQueries({ queryKey: ["issue", issue.key] });
      qc.invalidateQueries({ queryKey: ["issue", link.issue.key] });
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
      for (const k of ["risks", "decisions"]) qc.invalidateQueries({ queryKey: [k] });
      onDone();
    },
    onError: (err) => toast.error(err),
  });
  useEscapeLayer(true, onDone);
  const linked = issue.links.filter((l) => l.type === option.type && l.direction === option.direction).map((l) => l.issue.key);
  return (
    <div className="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-line bg-surface-2/60 p-2 sm:flex-nowrap">
      <div className="w-full shrink-0 sm:w-40">
        <Select aria-label="Link type" value={option.id} onChange={(e) => setKind(e.target.value)}>
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </Select>
      </div>
      <IssuePicker autoFocus scope={option.scope} exclude={[issue.key, ...linked]} onPick={(i) => create.mutate(i.key)} onCancel={onDone} />
      <Button size="md" variant="ghost" onClick={onDone} loading={create.isPending}>
        Cancel
      </Button>
    </div>
  );
}
