import { type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { fieldsQuery, useMe } from "@/api/queries";
import type { IssueDetail, Priority, User } from "@/api/types";
import { Popover } from "@/components/ui";
import { IssueTypeIcon } from "@/components/issue-bits";
import { formatDateTime, relativeTime } from "@/lib/format";
import { cn } from "@/lib/cn";
import { releasesQuery, sprintsQuery } from "./issue-api";
import { IssueKeyLink, useIssue } from "./issue-context";
import { ghost, InlineDate, InlineLabels, InlineMulti, InlineSelect, InlineText, InlineUser, MenuKeys, None } from "./inline-edit";
import { CustomFieldInline, fieldsFor, priorityOptions } from "./field-values";
import { IssuePicker } from "./IssuePicker";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[7.5rem_minmax(0,1fr)] items-start gap-x-3">
      <dt className="truncate pt-1.5 text-[13px] text-muted" title={label}>
        {label}
      </dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

/** DetailsPanel is the issue's field sidebar; every field saves on its own with a PATCH. */
export function DetailsPanel() {
  const { issue, update } = useIssue();
  const { data: me } = useMe();
  const { data: defs = [] } = useQuery(fieldsQuery);
  const level = issue.type.level;
  const { data: sprints = [] } = useQuery({ ...sprintsQuery(issue.projectKey), enabled: level === 0 });
  const { data: releases = [] } = useQuery(releasesQuery(issue.projectKey));
  const p = issue.permissions;
  const custom = fieldsFor(defs, issue.projectKey, issue.type.key);

  const assign = (u: User | null) => update({ assigneeId: u?.id ?? null }, { assignee: u ?? undefined });
  const canAssignSelf = !!me && issue.assignee?.id !== me.id && (p.assign || p.edit || p.transition);

  const sprintOptions = sprints
    .filter((s) => s.state !== "closed" || s.id === issue.sprintId)
    .map((s) => ({ value: s.id, label: s.name, hint: s.state === "active" ? "active" : s.state === "closed" ? "closed" : undefined }));
  const releaseOptions = releases
    .filter((r) => r.state === "unreleased" || issue.releases.some((x) => x.id === r.id))
    .map((r) => ({ value: r.id, label: r.name, hint: r.state !== "unreleased" ? r.state : r.releaseDate ? `due ${r.releaseDate}` : undefined }));

  return (
    <div className="@container rounded-lg border border-line bg-surface">
      <h2 className="border-b border-line px-4 py-2.5 text-sm font-semibold">Details</h2>
      <dl className="grid gap-x-8 gap-y-1.5 px-4 py-3 @xl:grid-cols-2">
        <Row label="Assignee">
          <InlineUser label="Assignee" value={issue.assignee} project={issue.projectKey} disabled={!p.assign} onCommit={assign} />
          {canAssignSelf && (
            <button type="button" onClick={() => assign(me)} className="mb-1 text-xs text-accent hover:underline">
              Assign to me
            </button>
          )}
        </Row>
        <Row label="Reporter">
          <InlineUser
            label="Reporter"
            value={issue.reporter}
            allowNone={false}
            noneLabel="None"
            disabled={!p.edit}
            onCommit={(u) => u && update({ reporterId: u.id }, { reporter: u })}
          />
        </Row>
        <Row label="Priority">
          <InlineSelect label="Priority" value={issue.priority} options={priorityOptions} disabled={!p.edit} onCommit={(v) => v && update({ priority: v as Priority }, { priority: v as Priority })} />
        </Row>
        {level === 0 && <ParentRow />}
        {level < 0 && issue.parent && (
          <Row label="Parent">
            <div className="flex min-h-8 items-center gap-2 text-sm">
              <IssueTypeIcon type={issue.parent.type} />
              <IssueKeyLink issueKey={issue.parent.key} className="truncate">
                {issue.parent.key} {issue.parent.summary}
              </IssueKeyLink>
            </div>
          </Row>
        )}
        {level === 0 && (sprintOptions.length > 0 || issue.sprint) && (
          <Row label="Sprint">
            <InlineSelect
              label="Sprint"
              value={issue.sprintId ?? null}
              options={sprintOptions}
              allowNone
              noneLabel="Backlog"
              disabled={!p.edit}
              onCommit={(v) => update({ sprintId: v }, { sprintId: v ?? undefined, sprint: sprints.find((s) => s.id === v) })}
            />
          </Row>
        )}
        {level < 0 && issue.sprint && (
          <Row label="Sprint">
            <p className="flex min-h-8 items-center text-sm" title="Sub-tasks follow their parent's sprint">
              {issue.sprint.name}
            </p>
          </Row>
        )}
        <Row label="Fix versions">
          <InlineMulti
            label="Fix versions"
            values={issue.releases.map((r) => r.id)}
            options={releaseOptions}
            disabled={!p.edit}
            empty="No unreleased versions. Create one on the Releases page."
            onCommit={(ids) => update({ releaseIds: ids }, { releases: releases.filter((r) => ids.includes(r.id)) })}
          />
        </Row>
        <Row label="Labels">
          <InlineLabels value={issue.labels} projectKey={issue.projectKey} disabled={!p.edit} onCommit={(labels) => update({ labels }, { labels })} />
        </Row>
        {level >= 0 && (
          <Row label="Story points">
            <InlineText
              label="Story points"
              type="number"
              value={issue.storyPoints != null ? String(issue.storyPoints) : ""}
              disabled={!p.edit}
              onCommit={(v) => {
                const n = v === "" ? null : Number(v);
                if (n !== null && Number.isNaN(n)) return;
                update({ storyPoints: n }, { storyPoints: n ?? undefined });
              }}
            />
          </Row>
        )}
        <Row label="Start date">
          <InlineDate label="Start date" value={issue.startDate} disabled={!p.edit} onCommit={(v) => update({ startDate: v }, { startDate: v ?? undefined })} />
        </Row>
        <Row label="Due date">
          <div className={cn(isOverdue(issue) && "text-danger")}>
            <InlineDate label="Due date" value={issue.dueDate} disabled={!p.edit} onCommit={(v) => update({ dueDate: v }, { dueDate: v ?? undefined })} />
          </div>
        </Row>
        {custom.map((def) => (
          <Row key={def.id} label={def.name}>
            <CustomFieldInline
              def={def}
              value={issue.fields[def.key]}
              projectKey={issue.projectKey}
              disabled={!p.edit}
              onCommit={(v) => {
                const next = { ...issue.fields };
                if (v === null) delete next[def.key];
                else next[def.key] = v;
                update({ fields: { [def.key]: v } }, { fields: next });
              }}
            />
          </Row>
        ))}
      </dl>
      <div className="flex flex-wrap gap-x-4 gap-y-1 border-t border-line px-4 py-2.5 text-xs text-muted">
        <span title={formatDateTime(issue.createdAt)}>Created {relativeTime(issue.createdAt)}</span>
        <span title={formatDateTime(issue.updatedAt)}>Updated {relativeTime(issue.updatedAt)}</span>
        {issue.resolvedAt && (
          <span title={formatDateTime(issue.resolvedAt)}>
            Resolved {relativeTime(issue.resolvedAt)}
            {issue.resolution && <> as {issue.resolution}</>}
          </span>
        )}
      </div>
    </div>
  );
}

function isOverdue(issue: IssueDetail) {
  if (!issue.dueDate || issue.status.category === "done") return false;
  return issue.dueDate < new Date().toISOString().slice(0, 10);
}

/** ParentRow picks the epic a standard issue belongs to. */
function ParentRow() {
  const { issue, update } = useIssue();
  const editable = issue.permissions.edit;
  const parent = issue.parent;
  return (
    <Row label="Parent">
      <div className="min-w-0 [&>div]:w-full">
        <Popover
          align="end"
          trigger={({ toggle, open }) => (
            <button type="button" className={ghost} disabled={!editable} onClick={toggle} aria-label="Parent epic" aria-haspopup="dialog" aria-expanded={open}>
              {parent ? (
                <>
                  <IssueTypeIcon type={parent.type} />
                  <span className="truncate">
                    <span className="text-muted">{parent.key}</span> {parent.summary}
                  </span>
                </>
              ) : (
                <None />
              )}
            </button>
          )}
          content={(close) => (
            <MenuKeys close={close} className="w-80">
              <IssuePicker
                autoFocus
                inline
                scope={`project = ${issue.projectKey} AND level = 1`}
                exclude={parent ? [parent.key] : []}
                placeholder="Search epics…"
                onCancel={close}
                onPick={(epic) => {
                  close();
                  update({ parentKey: epic.key }, { parent: { id: epic.id, key: epic.key, summary: epic.summary, type: epic.type, status: epic.status } });
                }}
              />
              {parent && (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    close();
                    update({ parentKey: null }, { parent: undefined });
                  }}
                  className="mt-1 flex w-full items-center gap-2 rounded px-2.5 py-1.5 text-left text-sm text-muted hover:bg-surface-2 hover:text-ink"
                >
                  <X className="size-3.5" /> Remove from {parent.key}
                </button>
              )}
            </MenuKeys>
          )}
        />
      </div>
    </Row>
  );
}
