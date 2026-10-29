import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, X } from "lucide-react";
import { IconButton, Input, Select, Textarea } from "@/components/ui";
import { ACTIONS, CATEGORIES, emptyAction, SET_FIELDS, TARGETS, type Action, type ActionType, type Category, type Target } from "./meta";

const templateVars: { v: string; hint: string }[] = [
  { v: "{{issue.key}}", hint: "the issue the action runs on" },
  { v: "{{issue.summary}}", hint: "the issue the action runs on" },
  { v: "{{trigger.key}}", hint: "the issue that fired the rule" },
  { v: "{{trigger.summary}}", hint: "the issue that fired the rule" },
  { v: "{{event.url}}", hint: "pull request / pipeline link" },
  { v: "{{event.to}}", hint: "status entered" },
];

/** ActionRow edits one action: its type, which issue(s) it applies to, and its options. */
export function ActionRow({
  action: a,
  index,
  count,
  onChange,
  onMove,
  onRemove,
  statuses,
  customFields,
  missing,
  chatChannels,
  projectKey,
  error,
}: {
  action: Action;
  index: number;
  count: number;
  onChange: (a: Action) => void;
  onMove: (d: -1 | 1) => void;
  onRemove: () => void;
  statuses: string[];
  customFields: string[];
  missing: Set<string>;
  chatChannels: { id: string; name: string; provider: string }[];
  projectKey: string;
  error?: string;
}) {
  const label = `Action ${index + 1}`;
  const target: Target = a.target ?? "issue";
  return (
    <li className="rounded-md border border-line bg-surface-2/50 p-3">
      <div className="mb-2.5 flex flex-wrap items-center gap-2">
        <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-surface-3 text-[11px] font-semibold">{index + 1}</span>
        <Select aria-label={`${label} type`} value={a.type} onChange={(e) => onChange(emptyAction(e.target.value as ActionType, target))} className="h-7.5 w-52 py-0 text-[13px]">
          {ACTIONS.map((x) => (
            <option key={x.type} value={x.type}>
              {x.label}
            </option>
          ))}
        </Select>
        <label className="flex items-center gap-1.5 text-xs text-muted">
          on
          <Select
            aria-label={`${label}: apply to`}
            value={target}
            onChange={(e) => onChange({ ...a, target: e.target.value as Target })}
            className="h-7.5 w-36 py-0 text-[13px]"
            title={TARGETS.find((t) => t.id === target)?.hint}
          >
            {TARGETS.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </Select>
        </label>
        <div className="ml-auto flex">
          <IconButton label={`Move action ${index + 1} up`} size="sm" disabled={index === 0} onClick={() => onMove(-1)}>
            <ArrowUp className="size-3.5" />
          </IconButton>
          <IconButton label={`Move action ${index + 1} down`} size="sm" disabled={index === count - 1} onClick={() => onMove(1)}>
            <ArrowDown className="size-3.5" />
          </IconButton>
          <IconButton label={`Remove action ${index + 1}`} size="sm" disabled={count === 1} onClick={onRemove}>
            <X className="size-3.5" />
          </IconButton>
        </div>
      </div>
      <Options action={a} label={label} onChange={onChange} statuses={statuses} customFields={customFields} missing={missing} chatChannels={chatChannels} projectKey={projectKey} />
      {target === "children" && a.type !== "rollup_dates" && <p className="mt-1.5 text-xs text-muted">Runs once for each child; nothing happens if the issue has none.</p>}
      {target === "parent" && a.type !== "rollup_dates" && <p className="mt-1.5 text-xs text-muted">Runs on the parent (a story's epic, a sub-task's story); nothing happens without one.</p>}
      {error && <p className="mt-1.5 text-xs text-danger">{error}</p>}
    </li>
  );
}

function Options({
  action: a,
  label,
  onChange,
  statuses,
  customFields,
  missing,
  chatChannels,
  projectKey,
}: {
  action: Action;
  label: string;
  onChange: (a: Action) => void;
  statuses: string[];
  customFields: string[];
  missing: Set<string>;
  chatChannels: { id: string; name: string; provider: string }[];
  projectKey: string;
}) {
  switch (a.type) {
    case "transition": {
      const value = a.category ? `cat:${a.category}` : a.status ? `status:${a.status}` : "";
      const unknown = !a.category && a.status && missing.has(a.status.toLowerCase());
      return (
        <div>
          <Select
            aria-label={`${label}: move to`}
            value={value}
            invalid={!value || !!unknown}
            onChange={(e) => {
              const v = e.target.value;
              const { status: _s, category: _c, ...rest } = a;
              void _s;
              void _c;
              onChange(v.startsWith("cat:") ? { ...rest, category: v.slice(4) as Category } : { ...rest, status: v.slice(7) });
            }}
          >
            <option value="" disabled>
              Choose where to move it…
            </option>
            <optgroup label="A status category (first allowed transition)">
              {CATEGORIES.map((c) => (
                <option key={c.id} value={`cat:${c.id}`}>
                  Any {c.label} status
                </option>
              ))}
            </optgroup>
            <optgroup label="A specific status">
              {a.status && !statuses.some((s) => s.toLowerCase() === a.status!.toLowerCase()) && (
                <option value={`status:${a.status}`}>{`${a.status}${unknown ? " (not in this project)" : ""}`}</option>
              )}
              {statuses.map((s) => (
                <option key={s} value={`status:${s}`}>
                  {s}
                </option>
              ))}
            </optgroup>
          </Select>
          <p className="mt-1 text-xs text-muted">
            {a.category
              ? "Runs the first transition the workflow allows into a status of this category; nothing happens if it is already there."
              : "Runs a transition into this status if the workflow allows one from the current status."}
          </p>
          {unknown && <p className="mt-1 text-xs text-warning">“{a.status}” is not a status in this project's workflows; pick one above or use a category.</p>}
        </div>
      );
    }
    case "rollup_dates":
      return (
        <p className="text-[13px] text-muted">
          Sets the start and due dates of {a.target === "children" ? "each child" : a.target === "parent" ? "the parent" : "the issue"} to span the earliest start and latest due date of its children.
          Usually applied to the parent, so epics cover their stories on the timeline.
        </p>
      );
    case "assign": {
      const preset = ["actor", "reporter", "unassigned"].includes(a.assignee ?? "") ? a.assignee : "email";
      return (
        <div className="flex gap-2">
          <Select aria-label={`${label}: assignee`} value={preset} onChange={(e) => onChange({ ...a, assignee: e.target.value === "email" ? "" : e.target.value })} className="max-w-xs">
            <option value="actor">Who triggered the event</option>
            <option value="reporter">The reporter</option>
            <option value="unassigned">Nobody (unassign)</option>
            <option value="email">A specific person…</option>
          </Select>
          {preset === "email" && (
            <Input aria-label={`${label}: email`} type="email" value={a.assignee ?? ""} onChange={(e) => onChange({ ...a, assignee: e.target.value })} placeholder="person@example.com" />
          )}
        </div>
      );
    }
    case "set_field":
      return (
        <div className="grid grid-cols-2 gap-2">
          <Select aria-label={`${label}: field`} value={a.field ?? ""} onChange={(e) => onChange({ ...a, field: e.target.value })}>
            {[...SET_FIELDS, ...customFields].map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </Select>
          {a.field === "priority" ? (
            <Select aria-label={`${label}: value`} value={String(a.value ?? "")} onChange={(e) => onChange({ ...a, value: e.target.value })}>
              {["highest", "high", "medium", "low", "lowest"].map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </Select>
          ) : (
            <Input
              aria-label={`${label}: value`}
              value={a.value === undefined || a.value === null ? "" : String(a.value)}
              type={a.field === "storyPoints" ? "number" : "text"}
              onChange={(e) => onChange({ ...a, value: a.field === "storyPoints" ? (e.target.value === "" ? null : Number(e.target.value)) : e.target.value })}
              placeholder="Value (templates allowed)"
            />
          )}
        </div>
      );
    case "comment":
      return (
        <div>
          <Textarea
            aria-label={`${label}: comment`}
            rows={3}
            value={a.body ?? ""}
            onChange={(e) => onChange({ ...a, body: e.target.value })}
            placeholder="Merged in {{event.url}} — closing {{issue.key}}."
            invalid={!a.body?.trim()}
          />
          <TemplateVars onInsert={(v) => onChange({ ...a, body: `${a.body ?? ""}${v}` })} />
        </div>
      );
    case "chat":
      return (
        <div className="flex flex-col gap-2">
          <Select aria-label={`${label}: channel`} value={a.channelId ?? ""} onChange={(e) => onChange({ ...a, channelId: e.target.value })} invalid={!a.channelId}>
            <option value="">{chatChannels.length ? "Choose a channel…" : "No chat channels in this project yet"}</option>
            {chatChannels.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name} ({c.provider})
              </option>
            ))}
          </Select>
          {chatChannels.length === 0 && (
            <p className="text-xs text-muted">
              Add a Slack or Mattermost channel in{" "}
              <Link to="/projects/$projectKey/settings/chat" params={{ projectKey }} className="text-accent hover:underline">
                Settings → Chat
              </Link>{" "}
              first.
            </p>
          )}
          <Textarea
            aria-label={`${label}: message`}
            rows={2}
            value={a.body ?? ""}
            onChange={(e) => onChange({ ...a, body: e.target.value })}
            invalid={!a.body?.trim()}
            placeholder="{{issue.key}} is blocked: {{issue.summary}}"
          />
          <TemplateVars onInsert={(v) => onChange({ ...a, body: `${a.body ?? ""}${v}` })} />
        </div>
      );
    case "add_label":
      return (
        <Input
          aria-label={`${label}: label`}
          value={a.label ?? ""}
          onChange={(e) => onChange({ ...a, label: e.target.value.replace(/\s+/g, "-") })}
          placeholder="needs-triage"
          invalid={!a.label?.trim()}
        />
      );
    case "webhook":
      return <WebhookFields a={a} label={label} onChange={onChange} />;
    case "notify":
      return (
        <Select aria-label={`${label}: recipients`} value={a.recipients ?? "watchers"} onChange={(e) => onChange({ ...a, recipients: e.target.value })} className="max-w-xs">
          <option value="assignee">The assignee</option>
          <option value="reporter">The reporter</option>
          <option value="watchers">Everyone watching</option>
        </Select>
      );
  }
}

/** WebhookFields edits a webhook action. Secrets are write-only: the API only says whether one is
 * stored. Leaving a secret empty keeps it, "Remove" sends "" and a new URL drops stored secrets. */
function WebhookFields({ a, label, onChange }: { a: Action; label: string; onChange: (a: Action) => void }) {
  const [savedUrl] = useState(a.url);
  const urlChanged = a.url !== savedUrl;
  const stored = (set: boolean | undefined, value: string | undefined) => !!set && !urlChanged && value === undefined;
  const secretStored = stored(a.secretSet, a.secret);
  const authStored = stored(a.authSet, a.authValue);
  return (
    <div className="space-y-2">
      <div>
        <Input aria-label={`${label}: URL`} type="url" value={a.url ?? ""} onChange={(e) => onChange({ ...a, url: e.target.value })} placeholder="https://hooks.example.com/automation" />
        <p className="mt-1 text-xs text-muted">Receives a JSON POST with the issue and the event.</p>
      </div>
      <SecretInput
        label={`${label}: signing secret`}
        title="Signing secret"
        hint="Signs the body as X-WFM-Signature-256, like outbound webhooks."
        stored={secretStored}
        value={a.secret}
        onChange={(secret) => onChange({ ...a, secret })}
      />
      <div className="grid gap-2 sm:grid-cols-[12rem_1fr]">
        <Input
          aria-label={`${label}: auth header`}
          value={a.authHeader ?? ""}
          onChange={(e) => onChange({ ...a, authHeader: e.target.value.trim() || undefined })}
          placeholder="Authorization"
          spellCheck={false}
          className="font-mono text-[13px]"
        />
        <SecretInput
          label={`${label}: auth value`}
          stored={authStored}
          value={a.authValue}
          placeholder="Bearer …"
          onChange={(authValue) => onChange({ ...a, authValue })}
        />
      </div>
      <p className="text-xs text-muted">
        Secrets are stored encrypted and never shown again.{urlChanged && (a.secretSet || a.authSet) ? " Changing the URL drops the stored secrets: enter them again to keep them." : ""}
      </p>
    </div>
  );
}

function SecretInput({
  label,
  title,
  hint,
  stored,
  value,
  placeholder,
  onChange,
}: {
  label: string;
  title?: string;
  hint?: string;
  stored: boolean;
  value: string | undefined;
  placeholder?: string;
  onChange: (v: string | undefined) => void;
}) {
  const removing = value === "";
  return (
    <div>
      {title && <p className="mb-1 text-xs font-medium">{title}</p>}
      <div className="flex items-center gap-2">
        <Input
          aria-label={label}
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={value ?? ""}
          disabled={removing}
          onChange={(e) => onChange(e.target.value || undefined)}
          placeholder={removing ? "Will be removed" : stored ? "Stored (type to replace)" : (placeholder ?? "Optional")}
          className="font-mono text-[13px]"
        />
        {(stored || removing) && (
          <button type="button" className="shrink-0 text-xs text-accent hover:underline" onClick={() => onChange(removing ? undefined : "")}>
            {removing ? "Keep" : "Remove"}
          </button>
        )}
      </div>
      {hint && <p className="mt-1 text-xs text-muted">{hint}</p>}
    </div>
  );
}

function TemplateVars({ onInsert }: { onInsert: (v: string) => void }) {
  return (
    <div className="text-xs text-muted">
      <p className="mt-1 flex flex-wrap items-center gap-1">
        Markdown. Insert:
        {templateVars.map(({ v, hint }) => (
          <button key={v} type="button" title={hint} onClick={() => onInsert(v)} className="rounded bg-surface-3 px-1 font-mono text-[11px] text-ink hover:bg-accent-soft">
            {v}
          </button>
        ))}
      </p>
      <p className="mt-0.5 text-faint">issue.* is the issue the action runs on; trigger.* is the issue that fired the rule.</p>
    </div>
  );
}
