import { useMemo, useState } from "react";
import { ArrowRight, RefreshCw, Save, Search, UserRoundCheck } from "lucide-react";
import type { User } from "@/api/types";
import { PRIORITIES } from "@/api/types";
import { Badge, Button, Card, Input, Select, table, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { UserPicker } from "@/components/UserPicker";
import { Callout, Switch } from "@/features/settings/common";
import { cn } from "@/lib/cn";
import { formatBytes } from "@/lib/format";
import { useImportActions } from "./api";
import { busy, CATEGORY_LABEL, LEVEL_LABEL, personOutcome, setPerson, setStatus, setType, type JiraConfig, type JiraImport, type PersonMapping } from "./model";
import { ScanProgress } from "./ProgressPanel";
import { Stat } from "./parts";

const categoryTone = { todo: "neutral", in_progress: "accent", done: "success" } as const;

/**
 * MappingStep shows the scan of the selected projects and the mapping of Jira values onto values
 * here, pre-filled by the server (statuses by name then category, types by hierarchy, people by
 * email) and editable.
 */
export function MappingStep({ imp, onNext }: { imp: JiraImport; onNext: () => void }) {
  const actions = useImportActions(imp.id);
  if (imp.status === "scanning") {
    return <ScanProgress imp={imp} />;
  }
  if (imp.scanStale || !imp.scan) {
    return (
      <Card title="Analyse the projects">
        <div className="flex flex-col items-start gap-3 p-4">
          <p className="text-sm text-muted">
            The analysis reads every issue of the selected projects once (without changing anything) to count them and to find the statuses, issue types, priorities and people the
            mapping needs.
          </p>
          {imp.stalled && <Callout tone="warning">The last analysis stopped unexpectedly. Start it again.</Callout>}
          {actions.scan.error && <ErrorBox error={actions.scan.error} />}
          <Button
            variant="primary"
            icon={<RefreshCw className="size-4" />}
            loading={actions.scan.isPending}
            disabled={busy(imp) || !imp.hasCredentials}
            onClick={() => actions.scan.mutate()}
          >
            Analyse {imp.config.projects.length} project{imp.config.projects.length === 1 ? "" : "s"}
          </Button>
        </div>
      </Card>
    );
  }
  // A new scan brings new defaults: start the editor afresh.
  return <MappingEditor key={imp.scan.scannedAt ?? ""} imp={imp} scan={imp.scan} onNext={onNext} />;
}

function MappingEditor({ imp, scan, onNext }: { imp: JiraImport; scan: NonNullable<JiraImport["scan"]>; onNext: () => void }) {
  const actions = useImportActions(imp.id);
  const toast = useToast();
  const [cfg, setCfg] = useState<JiraConfig>(imp.config);
  const [tab, setTab] = useState(imp.config.projects[0]?.jiraId ?? "");
  const [dirty, setDirty] = useState(false);
  const [continuing, setContinuing] = useState(false);
  const change = (next: JiraConfig) => (setCfg(next), setDirty(true));
  const target = cfg.projects.find((p) => p.jiraId === tab) ?? cfg.projects[0];
  const sp = scan.projects.find((p) => p.jiraId === target?.jiraId);
  const options = imp.targets.find((t) => t.jiraId === target?.jiraId);
  const save = async (next: boolean) => {
    setContinuing(next);
    const saved = await actions.saveConfig.mutateAsync(cfg);
    setCfg(saved.config);
    setDirty(false);
    if (next) onNext();
    else toast.success("Mapping saved");
  };
  const error = actions.saveConfig.error;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted">
        <p>
          Analysed {scan.projects.reduce((n, p) => n + p.issues, 0)} issues in {scan.projects.length} project{scan.projects.length === 1 ? "" : "s"}. Values are pre-filled; change
          what does not fit.
        </p>
        <Button size="sm" variant="ghost" icon={<RefreshCw className="size-3.5" />} loading={actions.scan.isPending} onClick={() => actions.scan.mutate()}>
          Analyse again
        </Button>
      </div>

      {cfg.projects.length > 1 && (
        <div role="tablist" aria-label="Project" className="flex flex-wrap gap-1">
          {cfg.projects.map((p) => (
            <button
              key={p.jiraId}
              type="button"
              role="tab"
              aria-selected={p.jiraId === target?.jiraId}
              onClick={() => setTab(p.jiraId)}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm",
                p.jiraId === target?.jiraId ? "bg-accent-soft font-medium text-accent" : "text-muted hover:bg-surface-2 hover:text-ink",
              )}
            >
              {p.jiraKey} → {options && p.jiraId === target?.jiraId && options.existingKey ? options.existingKey : p.key}
            </button>
          ))}
        </div>
      )}

      {target && sp && options && (
        <>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(9rem,1fr))] gap-2">
            <Stat label="Issues" value={sp.issues} hint={`highest number ${sp.maxNumber}`} />
            <Stat label="Comments" value={sp.comments} />
            <Stat label="Attachments" value={sp.attachments} hint={formatBytes(sp.attachmentBytes)} />
            <Stat label="Sprints" value={sp.sprints} hint={sp.boardType ? `${sp.boardType} board` : "no board"} />
            <Stat label="Versions" value={sp.versions} />
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <Card title={`Statuses · ${target.jiraKey}`}>
              <div className="overflow-x-auto">
                <table className={table}>
                  <thead>
                    <tr>
                      <th>In Jira</th>
                      <th className="w-14 text-right">Issues</th>
                      <th className="w-48">Here</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sp.statuses.map((st) => (
                      <tr key={st.id}>
                        <td>
                          <span className="mr-2">{st.name}</span>
                          <Badge tone={categoryTone[st.category]}>{CATEGORY_LABEL[st.category]}</Badge>
                        </td>
                        <td className="text-right text-muted tabular-nums">{st.count}</td>
                        <td>
                          <Select
                            className="min-w-36"
                            aria-label={`Status for ${st.name}`}
                            value={target.statuses[st.id] ?? ""}
                            onChange={(e) => change(setStatus(cfg, target.jiraId, st.id, e.target.value))}
                          >
                            {(["todo", "in_progress", "done"] as const).map((cat) => (
                              <optgroup key={cat} label={CATEGORY_LABEL[cat]}>
                                {options.statuses
                                  .filter((o) => o.category === cat)
                                  .map((o) => (
                                    <option key={o.name} value={o.name}>
                                      {o.name}
                                    </option>
                                  ))}
                              </optgroup>
                            ))}
                          </Select>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="border-t border-line px-4 py-2.5 text-xs text-muted">
                Where an issue type's workflow lacks the chosen status, the first status of the same category is used. History keeps the mapped statuses, so cycle time and flow
                charts cover the Jira past.
              </p>
            </Card>
            <Card title={`Issue types · ${target.jiraKey}`}>
              <div className="overflow-x-auto">
                <table className={table}>
                  <thead>
                    <tr>
                      <th>In Jira</th>
                      <th className="w-14 text-right">Issues</th>
                      <th className="w-48">Here</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sp.types.map((ty) => {
                      const chosen = options.types.find((o) => o.key === target.types[ty.id]);
                      return (
                        <tr key={ty.id}>
                          <td>
                            <span className="mr-2">{ty.name}</span>
                            <span className="text-xs text-faint">{LEVEL_LABEL[ty.level]}</span>
                            {chosen && chosen.level !== ty.level && <p className="text-xs text-warning">Different level: parent links that break the hierarchy are dropped.</p>}
                          </td>
                          <td className="text-right text-muted tabular-nums">{ty.count}</td>
                          <td>
                            <Select
                              className="min-w-36"
                              aria-label={`Type for ${ty.name}`}
                              value={target.types[ty.id] ?? ""}
                              onChange={(e) => change(setType(cfg, target.jiraId, ty.id, e.target.value))}
                            >
                              {options.types.map((o) => (
                                <option key={o.key} value={o.key}>
                                  {o.name}
                                </option>
                              ))}
                            </Select>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </Card>
          </div>
        </>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Priorities">
          <table className={table}>
            <thead>
              <tr>
                <th>In Jira</th>
                <th className="w-14 text-right">Issues</th>
                <th className="w-40">Here</th>
              </tr>
            </thead>
            <tbody>
              {scan.priorities.map((p) => (
                <tr key={p.id}>
                  <td>{p.name}</td>
                  <td className="text-right text-muted tabular-nums">{p.count}</td>
                  <td>
                    <Select
                      className="min-w-28"
                      aria-label={`Priority for ${p.name}`}
                      value={cfg.priorities[p.id] ?? "medium"}
                      onChange={(e) => change({ ...cfg, priorities: { ...cfg.priorities, [p.id]: e.target.value as (typeof PRIORITIES)[number] } })}
                    >
                      {PRIORITIES.map((v) => (
                        <option key={v} value={v}>
                          {v[0].toUpperCase() + v.slice(1)}
                        </option>
                      ))}
                    </Select>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
        <Card title="Fields and content">
          <div className="flex flex-col gap-4 p-4">
            <label className="flex flex-col gap-1.5 text-[13px] font-medium">
              Story points field
              <Select value={cfg.storyPointsField || "none"} onChange={(e) => change({ ...cfg, storyPointsField: e.target.value })}>
                {scan.storyPointFields.map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name} ({f.id}) · {f.count} issue{f.count === 1 ? "" : "s"}
                  </option>
                ))}
                <option value="none">Do not import story points</option>
              </Select>
              <span className="text-xs font-normal text-muted">Detected from fields named “Story Points” or “Story point estimate”; the one most issues use is chosen.</span>
            </label>
            <Switch checked={cfg.componentsAsLabels} onChange={(v) => change({ ...cfg, componentsAsLabels: v })} label="Import components as labels (component:<name>)" />
            <Switch checked={cfg.attachments} onChange={(v) => change({ ...cfg, attachments: v })} label="Download attachments" />
            {scan.oversizeCount > 0 && cfg.attachments && (
              <p className="text-xs text-warning">
                {scan.oversizeCount} attachment{scan.oversizeCount === 1 ? " is" : "s are"} over the size limit and will be skipped.
              </p>
            )}
          </div>
        </Card>
      </div>

      <PeopleCard imp={imp} cfg={cfg} onChange={change} />

      {error && <ErrorBox error={error} />}
      <div className="flex flex-wrap justify-end gap-2">
        <Button icon={<Save className="size-4" />} disabled={!dirty} loading={actions.saveConfig.isPending && !continuing} onClick={() => save(false).catch(() => undefined)}>
          Save mapping
        </Button>
        <Button variant="primary" icon={<ArrowRight className="size-4" />} loading={actions.saveConfig.isPending && continuing} onClick={() => save(true).catch(() => undefined)}>
          Continue to dry run
        </Button>
      </div>
    </div>
  );
}

function PeopleCard({ imp, cfg, onChange }: { imp: JiraImport; cfg: JiraConfig; onChange: (c: JiraConfig) => void }) {
  const [q, setQ] = useState("");
  const [picked, setPicked] = useState<Record<string, User>>({});
  const people = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return imp.people.filter((p) => !needle || p.displayName.toLowerCase().includes(needle) || p.email.toLowerCase().includes(needle));
  }, [imp.people, q]);
  const set = (id: string, m: PersonMapping | null) => onChange(setPerson(cfg, id, m));
  return (
    <Card
      title={`People (${imp.people.length})`}
      actions={
        imp.people.length > 8 && (
          <div className="relative w-56">
            <Search className="pointer-events-none absolute top-2 left-2.5 size-4 text-faint" aria-hidden />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter people" aria-label="Filter people" className="pl-8" />
          </div>
        )
      }
    >
      <Callout className="m-3" icon={<UserRoundCheck className="mt-0.5 size-4 shrink-0 text-accent" />} title="How people are brought over">
        People with an account here (same email) are matched. Everyone else becomes a <strong className="text-ink">placeholder</strong>: an inactive account with their name and
        email that owns their issues, comments and history. Nobody can sign in to a placeholder. When the person later signs in with that email through single sign-on (or is
        provisioned by SCIM or directory sync), the placeholder becomes their account with everything attached. People whose email Jira hides get a placeholder nobody can claim —
        enter their email or map them to an account.
      </Callout>
      <div className="max-h-[28rem] overflow-auto">
        <table className={table}>
          <thead className="sticky top-0 bg-surface">
            <tr>
              <th>In Jira</th>
              <th className="w-14 text-right">Uses</th>
              <th className="w-44">Becomes</th>
              <th className="w-60">
                <span className="sr-only">Details</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {people.map((p) => {
              const m = cfg.people[p.id];
              const action = m?.action ?? "auto";
              const outcome = personOutcome(p, m);
              const pickedUser =
                picked[p.id] ??
                (m?.userId && p.match?.id === m.userId
                  ? { ...p.match, kind: "human" as const }
                  : m?.userId
                    ? { id: m.userId, displayName: "Chosen account", email: "", kind: "human" as const }
                    : null);
              return (
                <tr key={p.id}>
                  <td>
                    <span className="flex flex-wrap items-center gap-1.5">
                      <span className="font-medium">{p.displayName}</span>
                      {p.app && <Badge>App</Badge>}
                      {!p.active && <Badge tone="warning">Inactive in Jira</Badge>}
                    </span>
                    <span className="block truncate text-xs text-muted">{p.email || "email hidden"}</span>
                  </td>
                  <td className="text-right text-muted tabular-nums">{p.count}</td>
                  <td>
                    <Select
                      aria-label={`Mapping for ${p.displayName}`}
                      value={action}
                      onChange={(e) => {
                        const a = e.target.value as PersonMapping["action"];
                        set(p.id, a === "auto" ? null : a === "placeholder" ? { action: a, email: p.email } : { action: a, userId: a === "user" ? m?.userId : undefined });
                      }}
                    >
                      <option value="auto">Automatic</option>
                      <option value="user">An account here…</option>
                      <option value="placeholder">Placeholder</option>
                      <option value="skip">Nobody</option>
                    </Select>
                  </td>
                  <td>
                    {action === "user" ? (
                      <UserPicker
                        value={pickedUser}
                        allowNone={false}
                        noneLabel="Choose an account…"
                        label={`Account for ${p.displayName}`}
                        onChange={(u) => {
                          if (!u) return;
                          setPicked({ ...picked, [p.id]: u });
                          set(p.id, { action: "user", userId: u.id });
                        }}
                      />
                    ) : action === "placeholder" ? (
                      <Input
                        type="email"
                        aria-label={`Placeholder email for ${p.displayName}`}
                        placeholder="Email they will sign in with"
                        value={m?.email ?? ""}
                        onChange={(e) => set(p.id, { action: "placeholder", email: e.target.value })}
                      />
                    ) : (
                      <span className={cn("text-[13px]", { success: "text-success", accent: "text-ink", warning: "text-warning", neutral: "text-muted" }[outcome.tone])}>
                        {outcome.text}
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
            {people.length === 0 && (
              <tr>
                <td colSpan={4} className="text-muted">
                  {q ? "No matching people." : "No people found in the analysed issues."}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
