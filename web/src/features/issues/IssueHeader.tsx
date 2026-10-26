import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Ellipsis, Eye, Link2, ListPlus, Maximize2, Paperclip, Repeat, Trash2, X } from "lucide-react";
import { api, unwrap } from "@/api/client";
import { hasPermission, issueTypesQuery, useMe, useProject } from "@/api/queries";
import type { IssueDetail } from "@/api/types";
import { IconButton, MenuItem, MenuLabel, Popover, useConfirm, useToast } from "@/components/ui";
import { IssueTypeIcon } from "@/components/issue-bits";
import { ProjectAvatar } from "@/layout/AppShell";
import { cn } from "@/lib/cn";
import { invalidateIssueLists } from "./issue-api";
import { useEscapeLayer } from "@/lib/escape";
import { IssueKeyLink, useIssue } from "./issue-context";
import { MenuKeys } from "./inline-edit";

/** IssueTopBar: breadcrumb (project › parent › key) and the issue's actions. */
export function IssueTopBar() {
  const { issue, variant, onClose } = useIssue();
  return (
    <div className={cn("flex h-12 items-center gap-2 px-4 sm:px-6", variant === "drawer" && "sticky top-0 z-20 border-b border-line bg-surface/95 backdrop-blur")}>
      <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-1 text-[13px] text-muted">
        <Link
          to="/projects/$projectKey"
          params={{ projectKey: issue.project.key }}
          className="flex min-w-0 items-center gap-1.5 rounded px-1 py-0.5 hover:bg-surface-2 hover:text-ink"
        >
          <ProjectAvatar projectKey={issue.project.key} size={16} />
          <span className="truncate">{issue.project.name}</span>
        </Link>
        {issue.parent && (
          <>
            <ChevronRight className="size-3.5 shrink-0 text-faint" />
            <span className="flex min-w-0 items-center gap-1.5 rounded px-1 py-0.5" title={issue.parent.summary}>
              <IssueTypeIcon type={issue.parent.type} />
              <IssueKeyLink issueKey={issue.parent.key} className="truncate hover:text-ink" />
            </span>
          </>
        )}
        <ChevronRight className="size-3.5 shrink-0 text-faint" />
        <span className="flex shrink-0 items-center gap-1.5 px-1 py-0.5 text-ink">
          <IssueTypeIcon type={issue.type} />
          <IssueKeyLink issueKey={issue.key} className="font-medium" />
        </span>
      </nav>
      <div className="flex shrink-0 items-center gap-0.5">
        <WatchButton issue={issue} />
        <CopyLinkButton issueKey={issue.key} />
        <MoreMenu />
        {variant === "drawer" && (
          <>
            <Link
              to="/browse/$issueKey"
              params={{ issueKey: issue.key }}
              aria-label="Open full page"
              title="Open full page"
              className="inline-flex size-8 items-center justify-center rounded-md text-muted hover:bg-surface-2 hover:text-ink"
            >
              <Maximize2 className="size-4" />
            </Link>
            {onClose && (
              <IconButton label="Close" size="sm" className="size-8" onClick={onClose}>
                <X className="size-4.5" />
              </IconButton>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function WatchButton({ issue }: { issue: IssueDetail }) {
  const qc = useQueryClient();
  const toast = useToast();
  const { data: me } = useMe();
  const key = ["issue", issue.key];
  const toggle = useMutation({
    mutationFn: (watch: boolean) =>
      watch
        ? unwrap(api.POST("/issues/{issueKey}/watchers", { params: { path: { issueKey: issue.key } }, body: {} }))
        : unwrap(api.DELETE("/issues/{issueKey}/watchers/{userId}", { params: { path: { issueKey: issue.key, userId: me!.id } } })),
    onMutate: (watch) => {
      const prev = qc.getQueryData<IssueDetail>(key);
      if (prev) qc.setQueryData<IssueDetail>(key, { ...prev, watching: watch, watcherCount: Math.max(0, prev.watcherCount + (watch ? 1 : -1)) });
      return { prev };
    },
    onError: (err, _w, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
      toast.error(err);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: key, exact: true }),
  });
  return (
    <button
      type="button"
      onClick={() => me && toggle.mutate(!issue.watching)}
      aria-pressed={issue.watching}
      title={issue.watching ? "Stop watching" : "Watch this issue"}
      className={cn(
        "inline-flex h-8 items-center gap-1.5 rounded-md px-2 text-[13px] hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent",
        issue.watching ? "text-accent" : "text-muted hover:text-ink",
      )}
    >
      <Eye className="size-4" strokeWidth={issue.watching ? 2.5 : 2} />
      <span className="tabular-nums">{issue.watcherCount}</span>
      <span className="sr-only">{issue.watching ? "watching, stop watching" : "watchers, start watching"}</span>
    </button>
  );
}

function CopyLinkButton({ issueKey }: { issueKey: string }) {
  const toast = useToast();
  return (
    <IconButton
      label="Copy link"
      size="sm"
      className="size-8"
      onClick={async () => {
        const url = `${window.location.origin}/browse/${issueKey}`;
        try {
          await navigator.clipboard.writeText(url);
          toast.success(`Link to ${issueKey} copied`);
        } catch {
          toast.info(url);
        }
      }}
    >
      <Link2 className="size-4" />
    </IconButton>
  );
}

function MoreMenu() {
  const { issue, variant, onClose, request, update } = useIssue();
  const qc = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const { data: types = [] } = useQuery(issueTypesQuery(issue.projectKey));
  const { data: project } = useProject(issue.projectKey);
  // Types of the same level and kind: work stays work, a risk stays a risk.
  const sameLevel = types.filter((t) => t.level === issue.type.level && t.kind === issue.type.kind && t.key !== issue.type.key);
  const childLevel = hasPermission(project, "issue.create") && issue.type.kind === "work" && types.some((t) => t.level === issue.type.level - 1);
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/issues/{issueKey}", { params: { path: { issueKey: issue.key } } })),
    onSuccess: () => {
      toast.success(`${issue.key} deleted`);
      qc.removeQueries({ queryKey: ["issue", issue.key] });
      if (issue.parent) qc.invalidateQueries({ queryKey: ["issue", issue.parent.key], exact: true });
      invalidateIssueLists(qc);
      if (variant === "drawer" && onClose) onClose();
      else navigate({ to: "/projects/$projectKey/issues", params: { projectKey: issue.projectKey } });
    },
    onError: (err) => toast.error(err),
  });
  const p = issue.permissions;
  const hasItems = p.edit || p.delete || p.comment;
  if (!hasItems) return null;
  return (
    <Popover
      align="end"
      trigger={({ toggle, open }) => (
        <IconButton label="More actions" size="sm" className="size-8" onClick={toggle} aria-haspopup="menu" aria-expanded={open}>
          <Ellipsis className="size-4.5" />
        </IconButton>
      )}
      content={(close) => (
        <MenuKeys close={close} className="w-56">
          {childLevel && issue.type.level >= 0 && (
            <MenuItem icon={<ListPlus className="size-4" />} onClick={() => (close(), request("add-child"))}>
              {issue.type.level === 0 ? "Add sub-task" : "Add child issue"}
            </MenuItem>
          )}
          {p.edit && (
            <MenuItem icon={<Link2 className="size-4" />} onClick={() => (close(), request("add-link"))}>
              Link issue
            </MenuItem>
          )}
          {p.comment && (
            <MenuItem icon={<Paperclip className="size-4" />} onClick={() => (close(), request("attach"))}>
              Attach files
            </MenuItem>
          )}
          {p.edit && sameLevel.length > 0 && (
            <>
              <MenuLabel>Change type</MenuLabel>
              {sameLevel.map((t) => (
                <MenuItem
                  key={t.id}
                  icon={<IssueTypeIcon type={t} />}
                  onClick={() => {
                    close();
                    update({ typeKey: t.key }, { type: { id: t.id, key: t.key, name: t.name, icon: t.icon, color: t.color, level: t.level, kind: t.kind } }).then(
                      (res) => res && toast.success(`${issue.key} is now a ${t.name.toLowerCase()}`),
                    );
                  }}
                >
                  <span className="flex-1">{t.name}</span>
                  <Repeat className="size-3.5 text-faint" />
                </MenuItem>
              ))}
            </>
          )}
          {p.delete && (
            <div className="mt-1 border-t border-line pt-1">
              <MenuItem
                danger
                icon={<Trash2 className="size-4" />}
                onClick={async () => {
                  close();
                  const n = issue.children.filter((c) => c.type.level < 0).length;
                  const ok = await confirm({
                    title: `Delete ${issue.key}?`,
                    body: (
                      <>
                        <span className="font-medium text-ink">{issue.summary}</span> will be deleted permanently
                        {n > 0 ? ` together with its ${n} sub-task${n === 1 ? "" : "s"}` : ""}. This cannot be undone.
                      </>
                    ),
                    confirmLabel: "Delete issue",
                    danger: true,
                  });
                  if (ok) remove.mutate();
                }}
              >
                Delete issue
              </MenuItem>
            </div>
          )}
        </MenuKeys>
      )}
    />
  );
}

/** SummaryField is the issue title, edited in place (Enter saves, Escape cancels). */
export function SummaryField() {
  const { issue, update } = useIssue();
  const [draft, setDraft] = useState<string | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const editable = issue.permissions.edit;
  useEscapeLayer(draft !== null, () => setDraft(null));
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [draft]);
  const save = () => {
    if (draft === null) return;
    const v = draft.replace(/\s+/g, " ").trim();
    setDraft(null);
    if (v && v !== issue.summary) update({ summary: v }, { summary: v });
  };
  const cls = "w-full text-[22px] leading-snug font-semibold tracking-tight";
  if (draft !== null) {
    return (
      <textarea
        ref={ref}
        autoFocus
        rows={1}
        maxLength={255}
        aria-label="Summary"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onFocus={(e) => e.currentTarget.select()}
        onBlur={save}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            save();
          }
        }}
        className={cn(cls, "-mx-2 -my-1 resize-none overflow-hidden rounded-md border border-accent bg-surface px-2 py-1 outline-none ring-2 ring-accent/25")}
      />
    );
  }
  if (!editable) return <h1 className={cls}>{issue.summary}</h1>;
  return (
    <h1 className={cls}>
      <button
        type="button"
        onClick={() => setDraft(issue.summary)}
        title="Click to edit"
        className="-mx-2 -my-1 w-[calc(100%+1rem)] rounded-md px-2 py-1 text-left hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent"
      >
        {issue.summary}
      </button>
    </h1>
  );
}
