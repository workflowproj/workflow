import { useId, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Pencil, Plus, Replace, X } from "lucide-react";
import { issueTypesQuery } from "@/api/queries";
import type { IssueDetail } from "@/api/types";
import { api, unwrap, type Schemas } from "@/api/client";
import { Button, Field, IconButton, Modal, Textarea, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { IssueTypeIcon, StatusBadge } from "@/components/issue-bits";
import { Markdown } from "@/components/Markdown";
import { MarkdownEditor } from "@/components/MarkdownEditor";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { IssueKeyLink, Section, useIssue } from "@/features/issues/issue-context";
import { IssuePicker } from "@/features/issues/IssuePicker";
import { InlineDate, InlineSelect } from "@/features/issues/inline-edit";
import { useEscapeLayer } from "@/lib/escape";
import { useSupersede } from "./api";
import { RatingBadge, WorkProgress } from "./bits";
import { adrFileName, adrMarkdown, assessRisk, DECISION_FIELDS, impactLabel, likelihoodLabel, linkedFrom, linkedTo, RESPONSES, RISK_FIELDS, textField } from "./model";

type IssueRef = Schemas["IssueRef"];

/** useFieldUpdate saves one custom field of the open issue (null clears it). */
function useFieldUpdate() {
  const { issue, update } = useIssue();
  return (key: string, value: unknown) => {
    const next = { ...issue.fields };
    if (value === null || value === "") delete next[key];
    else next[key] = value;
    return update({ fields: { [key]: value === "" ? null : value } }, { fields: next });
  };
}

function Card({ title, children, footer }: { title: string; children: ReactNode; footer?: ReactNode }) {
  return (
    <section aria-label={title} className="rounded-lg border border-line bg-surface">
      <h2 className="border-b border-line px-4 py-2.5 text-sm font-semibold">{title}</h2>
      <div className="flex flex-col gap-1.5 px-4 py-3">{children}</div>
      {footer && <div className="flex flex-wrap gap-2 border-t border-line px-4 py-2.5">{footer}</div>}
    </section>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[7.5rem_minmax(0,1fr)] items-start gap-x-3">
      <span className="truncate pt-1.5 text-[13px] text-muted">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

const levels = (label: (n: number) => string) => [1, 2, 3, 4, 5].map((n) => ({ value: String(n), label: label(n) }));

/** RiskPanel is a risk's assessment: likelihood and impact (which set the score), and the response. */
export function RiskPanel() {
  const { issue } = useIssue();
  const save = useFieldUpdate();
  const a = assessRisk(issue.fields);
  const disabled = !issue.permissions.edit;
  return (
    <Card title="Risk">
      <Row label="Score">
        <div className="flex min-h-8 items-center">
          <RatingBadge score={a.score} rating={a.rating} className="h-6 px-2 text-xs" />
        </div>
      </Row>
      <Row label="Likelihood">
        <InlineSelect label="Likelihood" value={a.likelihood ? String(a.likelihood) : null} options={levels(likelihoodLabel)} allowNone noneLabel="Not assessed" disabled={disabled} onCommit={(v) => save(RISK_FIELDS.likelihood, v ? Number(v) : null)} />
      </Row>
      <Row label="Impact">
        <InlineSelect label="Impact" value={a.impact ? String(a.impact) : null} options={levels(impactLabel)} allowNone noneLabel="Not assessed" disabled={disabled} onCommit={(v) => save(RISK_FIELDS.impact, v ? Number(v) : null)} />
      </Row>
      <Row label="Response">
        <InlineSelect
          label="Response"
          value={a.response ?? null}
          options={RESPONSES.map((r) => ({ value: r, label: r }))}
          allowNone
          noneLabel="Not decided"
          disabled={disabled}
          onCommit={(v) => save(RISK_FIELDS.response, v)}
        />
      </Row>
    </Card>
  );
}

/**
 * TextFieldSection shows a long text field as Markdown and edits it in place, like the
 * description (the mitigation plan, the sections of a decision record).
 */
function TextFieldSection({ title, fieldKey, placeholder, empty }: { title: string; fieldKey: string; placeholder: string; empty: string }) {
  const { issue } = useIssue();
  const save = useFieldUpdate();
  const value = textField(issue.fields, fieldKey);
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const editable = issue.permissions.edit;
  useEscapeLayer(draft !== null, () => draft?.trim() === value.trim() && setDraft(null));
  const commit = async () => {
    if (draft === null) return;
    if (draft.trim() === value.trim()) return setDraft(null);
    setSaving(true);
    const res = await save(fieldKey, draft.trim() || null);
    setSaving(false);
    if (res) setDraft(null);
  };
  return (
    <Section
      title={title}
      actions={
        editable &&
        draft === null &&
        value && (
          <IconButton label={`Edit ${title.toLowerCase()}`} size="sm" onClick={() => setDraft(value)}>
            <Pencil className="size-3.5" />
          </IconButton>
        )
      }
    >
      {draft !== null ? (
        <div>
          <MarkdownEditor label={title} value={draft} onChange={setDraft} autoFocus onSubmit={commit} minHeight={90} placeholder={placeholder} />
          <div className="mt-2 flex items-center gap-2">
            <Button size="sm" variant="primary" onClick={commit} loading={saving}>
              Save
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDraft(null)} disabled={saving}>
              Cancel
            </Button>
          </div>
        </div>
      ) : value ? (
        <div
          className={editable ? "-mx-2 cursor-text rounded-md px-2 py-1 hover:bg-surface-2/60" : undefined}
          onClick={(e) => {
            if (!editable || (e.target as HTMLElement).closest("a, button") || window.getSelection()?.toString()) return;
            setDraft(value);
          }}
        >
          <Markdown>{value}</Markdown>
        </div>
      ) : editable ? (
        <button
          type="button"
          onClick={() => setDraft("")}
          className="w-full rounded-md border border-dashed border-line-strong px-3 py-2.5 text-left text-sm text-faint hover:border-accent hover:text-muted"
        >
          {placeholder}
        </button>
      ) : (
        <p className="text-sm text-faint">{empty}</p>
      )}
    </Section>
  );
}

/** LinkedWork lists the issues linked to this one (mitigating work); people who can edit unlink them here. */
function LinkedWork({ links, empty }: { links: Schemas["IssueLink"][]; empty: string }) {
  const { issue } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const unlink = useMutation({
    mutationFn: (l: Schemas["IssueLink"]) => unwrap(api.DELETE("/issue-links/{linkId}", { params: { path: { linkId: l.id } } })),
    onSuccess: (_d, l) => {
      for (const k of [issue.key, l.issue.key]) qc.invalidateQueries({ queryKey: ["issue", k] });
      qc.invalidateQueries({ queryKey: ["issue-history", issue.key] });
      qc.invalidateQueries({ queryKey: ["risks", issue.projectKey] });
    },
    onError: (err) => toast.error(err),
  });
  if (links.length === 0) return <p className="text-sm text-faint">{empty}</p>;
  return (
    <ul className="overflow-hidden rounded-md border border-line">
      {links.map(({ id, issue: i }, n) => (
        <li key={id} className="group flex items-center gap-2.5 border-b border-line px-3 py-2 text-sm last:border-b-0 hover:bg-surface-2">
          <IssueTypeIcon type={i.type} />
          <IssueKeyLink issueKey={i.key} className="shrink-0 font-mono text-xs text-muted" />
          <IssueKeyLink issueKey={i.key} className="min-w-0 flex-1 truncate text-ink">
            {i.summary}
          </IssueKeyLink>
          <StatusBadge status={i.status} className="shrink-0" />
          {issue.permissions.edit && (
            <IconButton
              label={`Unlink ${i.key}`}
              size="sm"
              className="size-6 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
              disabled={unlink.isPending}
              onClick={() => unlink.mutate(links[n])}
            >
              <X className="size-3.5" />
            </IconButton>
          )}
        </li>
      ))}
    </ul>
  );
}

/** MitigationSection is a risk's mitigation plan and the work linked to it with mitigates. */
export function MitigationSection() {
  const { issue } = useIssue();
  const [creating, setCreating] = useState(false);
  const work = linkedFrom(issue.links, "mitigates");
  return (
    <>
      <TextFieldSection title="Mitigation plan" fieldKey={RISK_FIELDS.mitigation} placeholder="What reduces the likelihood or the impact, and who does it?" empty="No mitigation plan." />
      <Section
        title="Mitigating work"
        count={work.total}
        actions={
          issue.permissions.edit && (
            <Button size="sm" variant="subtle" icon={<Plus className="size-3.5" />} onClick={() => setCreating(true)}>
              Create mitigating issue
            </Button>
          )
        }
      >
        {work.total > 0 && <WorkProgress total={work.total} done={work.done} className="mb-2" />}
        <LinkedWork links={work.links} empty="No work mitigates this risk yet. Create an issue here, or link existing work with “is mitigated by”." />
      </Section>
      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={issue.projectKey} defaults={{ link: { type: "mitigates", direction: "outward", key: issue.key } }} />
    </>
  );
}

/** DecisionRecord is a decision's record: context, options considered, the decision and its consequences. */
export function DecisionRecord() {
  return (
    <>
      <TextFieldSection title="Context" fieldKey={DECISION_FIELDS.context} placeholder="The situation and the forces that call for a decision." empty="No context recorded." />
      <TextFieldSection title="Options considered" fieldKey={DECISION_FIELDS.options} placeholder="The alternatives weighed, with their pros and cons." empty="No options recorded." />
      <TextFieldSection title="Decision" fieldKey={DECISION_FIELDS.outcome} placeholder="What was decided, and why." empty="No decision recorded." />
      <TextFieldSection title="Consequences" fieldKey={DECISION_FIELDS.consequences} placeholder="What becomes easier or harder because of the decision." empty="No consequences recorded." />
    </>
  );
}

/** download saves text as a file. */
function download(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/markdown;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

/**
 * DecisionPanel is a decision's date, the decisions it replaces or that replace it, the work
 * implementing it, and the Supersede and Export as Markdown actions.
 */
export function DecisionPanel() {
  const { issue } = useIssue();
  const save = useFieldUpdate();
  const [superseding, setSuperseding] = useState(false);
  const supersededBy = linkedFrom(issue.links, "supersedes").issues;
  const supersedes = linkedTo(issue.links, "supersedes");
  const implementing = linkedFrom(issue.links, "implements");
  const canSupersede = issue.permissions.transition && issue.permissions.edit && issue.status.name === "Accepted";
  return (
    <Card
      title="Decision"
      footer={
        <>
          {canSupersede && (
            <Button size="sm" icon={<Replace className="size-3.5" />} onClick={() => setSuperseding(true)}>
              Supersede
            </Button>
          )}
          <Button size="sm" variant="subtle" icon={<Download className="size-3.5" />} onClick={() => download(adrFileName(issue), adrMarkdown(issue))}>
            Export as Markdown
          </Button>
        </>
      }
    >
      <Row label="Decided on">
        <InlineDate label="Decided on" value={textField(issue.fields, DECISION_FIELDS.date) || null} disabled={!issue.permissions.edit} onCommit={(v) => save(DECISION_FIELDS.date, v)} />
      </Row>
      <Row label="Superseded by">
        <RefList issues={supersededBy} />
      </Row>
      {supersedes.length > 0 && (
        <Row label="Supersedes">
          <RefList issues={supersedes} />
        </Row>
      )}
      <Row label="Implemented by">
        <div className="flex min-h-8 flex-col justify-center gap-1">
          <WorkProgress total={implementing.total} done={implementing.done} />
          {implementing.issues.length > 0 && <RefList issues={implementing.issues} />}
        </div>
      </Row>
      {superseding && <SupersedeDialog issue={issue} onClose={() => setSuperseding(false)} />}
    </Card>
  );
}

function RefList({ issues }: { issues: IssueRef[] }) {
  if (issues.length === 0) return <p className="flex min-h-8 items-center text-sm text-faint">None</p>;
  return (
    <ul className="flex flex-col gap-0.5 py-1">
      {issues.map((i) => (
        <li key={i.id} className="flex min-w-0 items-center gap-1.5 text-sm">
          <IssueKeyLink issueKey={i.key} className="shrink-0 font-mono text-xs text-muted" />
          <IssueKeyLink issueKey={i.key} className="truncate">
            {i.summary}
          </IssueKeyLink>
        </li>
      ))}
    </ul>
  );
}

/**
 * SupersedeDialog replaces an accepted decision with a newer one: an existing decision, or one
 * created on the spot. The decision is linked and marked superseded in one step.
 */
function SupersedeDialog({ issue, onClose }: { issue: IssueDetail; onClose: () => void }) {
  const { data: types = [] } = useQuery(issueTypesQuery(issue.projectKey));
  const decisionType = types.find((t) => t.kind === "decision");
  const supersede = useSupersede(issue.key, issue.projectKey);
  const [comment, setComment] = useState("");
  const [creating, setCreating] = useState(false);
  const pickerId = useId();
  const run = (by: string) => supersede.mutate({ by, comment }, { onSuccess: onClose });
  return (
    <>
      <Modal open={!creating} onClose={onClose} title={`Supersede ${issue.key}`} footer={<Button onClick={onClose}>Cancel</Button>}>
        <div className="flex flex-col gap-4 text-sm">
          <p className="text-muted">
            Pick the decision that replaces “{issue.summary}”, or create it. {issue.key} is linked to it and marked superseded.
          </p>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={pickerId} className="text-[13px] font-medium">
              Superseded by
            </label>
            <div className="flex flex-wrap items-center gap-2 sm:flex-nowrap">
              <IssuePicker id={pickerId} scope={`kind = decision AND project = ${issue.projectKey}`} exclude={[issue.key]} placeholder="Search decisions…" onPick={(i) => run(i.key)} onCancel={onClose} />
              {decisionType && (
                <Button icon={<Plus className="size-4" />} onClick={() => setCreating(true)} disabled={supersede.isPending}>
                  New decision
                </Button>
              )}
            </div>
          </div>
          <Field label="Comment" hint="Added to the superseded decision.">
            {(id) => <Textarea id={id} rows={2} value={comment} onChange={(e) => setComment(e.target.value)} />}
          </Field>
          {supersede.error && <ErrorBox error={supersede.error} />}
        </div>
      </Modal>
      <CreateIssueDialog
        open={creating}
        onClose={() => setCreating(false)}
        projectKey={issue.projectKey}
        defaults={{ typeKey: decisionType?.key, summary: issue.summary }}
        onCreated={(i) => {
          setCreating(false);
          run(i.key);
        }}
      />
    </>
  );
}
