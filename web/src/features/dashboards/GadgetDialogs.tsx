import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, ChartArea, ChartBarBig, ChartLine, Hash, HeartPulse, List, Timer, TrendingDown, TrendingUp } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import { projectsQuery } from "@/api/queries";
import { Button, Field, Input, Modal, Select } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { sprintsQuery } from "@/features/board/queries";
import { formatRange } from "@/features/backlog/dates";
import { JqlEditor, type JqlError } from "@/features/search/JqlEditor";
import { runeOffsetToIndex } from "@/features/search/jql";
import { ProjectPicker } from "@/features/timeline/Toolbar";
import { BREAKDOWNS, cleanConfig, defaultTitle, GADGETS, gadgetMeta, STATS, statMeta, WINDOWS, type Gadget, type GadgetConfig, type GadgetGroup, type GadgetOption, type GadgetType } from "./model";

export const GADGET_ICONS: Record<GadgetType, ReactNode> = {
  sprint_health: <HeartPulse className="size-5" />,
  sprint_burndown: <TrendingDown className="size-5" />,
  sprint_burnup: <TrendingUp className="size-5" />,
  velocity: <BarChart3 className="size-5" />,
  cumulative_flow: <ChartArea className="size-5" />,
  created_resolved: <ChartLine className="size-5" />,
  cycle_time: <Timer className="size-5" />,
  breakdown: <ChartBarBig className="size-5" />,
  stat: <Hash className="size-5" />,
  issue_list: <List className="size-5" />,
};

const GROUPS: GadgetGroup[] = ["Sprints", "Flow and trends", "Breakdowns and lists"];

/** GadgetGallery offers every gadget type with a line on what it shows. */
export function GadgetGallery({ open, onClose, onPick }: { open: boolean; onClose: () => void; onPick: (type: GadgetType) => void }) {
  return (
    <Modal open={open} onClose={onClose} title="Add a gadget" size="xl">
      <div className="flex flex-col gap-5">
        {GROUPS.map((group) => (
          <section key={group}>
            <h3 className="mb-2 text-xs font-semibold tracking-wide text-faint uppercase">{group}</h3>
            <ul className="grid gap-2 sm:grid-cols-2">
              {GADGETS.filter((g) => g.group === group).map((g) => (
                <li key={g.type}>
                  <button
                    type="button"
                    onClick={() => onPick(g.type)}
                    className="flex h-full w-full items-start gap-3 rounded-lg border border-line bg-surface p-3 text-left hover:border-accent hover:bg-accent-soft/40 focus-visible:outline-2 focus-visible:outline-accent"
                  >
                    <span className="mt-0.5 inline-flex size-9 shrink-0 items-center justify-center rounded-md bg-surface-2 text-accent">{GADGET_ICONS[g.type]}</span>
                    <span className="min-w-0">
                      <span className="block text-sm font-medium">{g.name}</span>
                      <span className="block text-[13px] text-muted">{g.description}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </Modal>
  );
}

const COUNTS = [3, 4, 5, 6, 7, 8, 10, 12, 15, 20];
const LIMITS = [5, 10, 15, 20, 25, 50];

/**
 * GadgetConfigDialog edits one gadget: title, width, scope (a project and sprint, or projects
 * and JQL) and the settings its type uses. The JQL is checked against the search API before
 * the change is applied, and errors point at the offending character.
 */
export function GadgetConfigDialog({ gadget, onClose, onSave }: { gadget: Gadget | null; onClose: () => void; onSave: (g: Gadget) => void }) {
  return <Modal open={!!gadget} onClose={onClose} title={gadget ? `${gadgetMeta(gadget.type).name} settings` : ""} size="lg">{gadget && <ConfigForm key={gadget.id} gadget={gadget} onClose={onClose} onSave={onSave} />}</Modal>;
}

function ConfigForm({ gadget, onClose, onSave }: { gadget: Gadget; onClose: () => void; onSave: (g: Gadget) => void }) {
  const meta = gadgetMeta(gadget.type);
  const [title, setTitle] = useState(gadget.title ?? "");
  const [width, setWidth] = useState(gadget.width);
  const [c, setC] = useState<GadgetConfig>(gadget.config);
  const [jqlError, setJqlError] = useState<JqlError | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const set = (patch: Partial<GadgetConfig>) => setC((prev) => ({ ...prev, ...patch }));
  const { data: projects = [] } = useQuery(projectsQuery);
  const sprints = useQuery({ ...sprintsQuery(c.projectKey ?? ""), enabled: meta.scope === "sprint" && !!c.projectKey });
  const has = (o: GadgetOption) => meta.options.includes(o);
  const showDays = has("days") && (gadget.type !== "stat" || statMeta(c.stat).windowed);
  useEffect(() => setJqlError(null), [c.jql]);

  const save = async () => {
    const config = cleanConfig(gadget.type, c);
    if (meta.scope === "query" && config.jql) {
      // Run the query once so a typo shows here rather than as a broken gadget.
      setChecking(true);
      setError(null);
      try {
        await unwrap(api.GET("/search", { params: { query: { jql: config.jql, limit: 1 } } }));
      } catch (e) {
        if (e instanceof ApiError && e.status === 400) setJqlError({ message: e.message, index: e.problem.position !== undefined ? runeOffsetToIndex(config.jql, e.problem.position) : undefined });
        else setError(e);
        return;
      } finally {
        setChecking(false);
      }
    }
    onSave({ ...gadget, title: title.trim() || undefined, width, config });
  };

  const placeholder = defaultTitle({ ...gadget, config: c });
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
      className="flex flex-col gap-4"
    >
      <div className="grid gap-4 sm:grid-cols-[1fr_11rem]">
        <Field label="Title" hint="Leave empty to name it from its settings.">
          {(id) => <Input id={id} value={title} maxLength={80} placeholder={placeholder} onChange={(e) => setTitle(e.target.value)} />}
        </Field>
        <Field label="Width">
          {(id) => (
            <Select id={id} value={width} onChange={(e) => setWidth(Number(e.target.value) as 1 | 2 | 3)}>
              <option value={1}>Narrow</option>
              <option value={2}>Wide</option>
              <option value={3}>Full width</option>
            </Select>
          )}
        </Field>
      </div>

      {meta.scope === "query" ? (
        <>
          <Group label="Projects" hint="None picked means every project the viewer can browse.">
            <ProjectPicker value={c.projectKeys ?? []} onChange={(keys) => set({ projectKeys: keys.length ? keys : undefined })} />
          </Group>
          <Group label="JQL" hint={gadget.type === "issue_list" ? "Optional. ORDER BY sets the list's order." : "Optional. Narrows the issues further."}>
            <JqlEditor
              value={c.jql ?? ""}
              onChange={(v) => set({ jql: v })}
              onRun={() => void save()}
              error={jqlError}
              projectKey={c.projectKeys?.[0]}
              placeholder={gadget.type === "issue_list" ? "statusCategory != Done ORDER BY priority DESC" : "type = Bug AND priority >= High"}
            />
          </Group>
        </>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Project" required>
            {(id) => (
              <Select id={id} value={c.projectKey ?? ""} onChange={(e) => set({ projectKey: e.target.value || undefined, sprintId: undefined })}>
                <option value="">Choose a project</option>
                {projects.map((p) => (
                  <option key={p.id} value={p.key}>
                    {p.name} ({p.key})
                  </option>
                ))}
              </Select>
            )}
          </Field>
          {meta.scope === "sprint" ? (
            <Field label="Sprint">
              {(id) => (
                <Select id={id} value={c.sprintId ?? ""} disabled={!c.projectKey} onChange={(e) => set({ sprintId: e.target.value || undefined })}>
                  <option value="">Active sprint (else the last completed)</option>
                  {(sprints.data ?? [])
                    .filter((s) => s.state !== "future")
                    .map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                        {s.state === "active" ? " (active)" : ""} {formatRange(s.startDate, s.endDate) && `· ${formatRange(s.startDate, s.endDate)}`}
                      </option>
                    ))}
                </Select>
              )}
            </Field>
          ) : (
            <Field label="Sprints shown">
              {(id) => (
                <Select id={id} value={c.count ?? 7} onChange={(e) => set({ count: Number(e.target.value) })}>
                  {COUNTS.map((n) => (
                    <option key={n} value={n}>
                      Last {n} completed
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
        </div>
      )}

      {(has("stat") || has("by") || showDays || has("interval") || has("flowBy") || has("measure") || has("chart") || has("limit")) && (
        <div className="grid gap-4 sm:grid-cols-2">
          {has("stat") && (
            <Field label="Number">
              {(id) => (
                <Select id={id} value={c.stat ?? "open"} onChange={(e) => set({ stat: e.target.value as GadgetConfig["stat"] })}>
                  {STATS.map((s) => (
                    <option key={s.value} value={s.value}>
                      {s.label}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
          {has("by") && (
            <Field label="Group by">
              {(id) => (
                <Select id={id} value={c.by ?? "assignee"} onChange={(e) => set({ by: e.target.value as GadgetConfig["by"] })}>
                  {BREAKDOWNS.map((b) => (
                    <option key={b.value} value={b.value}>
                      {b.label}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
          {showDays && (
            <Field label="Time window">
              {(id) => (
                <Select id={id} value={c.days ?? 30} onChange={(e) => set({ days: Number(e.target.value) })}>
                  {WINDOWS.map((d) => (
                    <option key={d} value={d}>
                      Last {d} days
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
          {has("interval") && (
            <Field label="Group into">
              {(id) => (
                <Select id={id} value={c.interval ?? "day"} onChange={(e) => set({ interval: e.target.value as "day" | "week" })}>
                  <option value="day">Days</option>
                  <option value="week">Weeks</option>
                </Select>
              )}
            </Field>
          )}
          {has("flowBy") && (
            <Field label="Bands">
              {(id) => (
                <Select id={id} value={c.flowBy ?? "category"} onChange={(e) => set({ flowBy: e.target.value as "category" | "status" })}>
                  <option value="category">Status category (to do, in progress, done)</option>
                  <option value="status">Each status</option>
                </Select>
              )}
            </Field>
          )}
          {has("measure") && (
            <Field label="Measure">
              {(id) => (
                <Select id={id} value={c.measure ?? ""} onChange={(e) => set({ measure: (e.target.value || undefined) as GadgetConfig["measure"] })}>
                  {meta.scope !== "query" && <option value="">Story points, or issues if not estimated</option>}
                  <option value="issues">Issue count</option>
                  <option value="points">Story points</option>
                </Select>
              )}
            </Field>
          )}
          {has("chart") && (
            <Field label="Chart">
              {(id) => (
                <Select id={id} value={c.chart ?? "bar"} onChange={(e) => set({ chart: e.target.value as "bar" | "donut" })}>
                  <option value="bar">Bars</option>
                  <option value="donut">Donut</option>
                </Select>
              )}
            </Field>
          )}
          {has("limit") && (
            <Field label="Rows">
              {(id) => (
                <Select id={id} value={c.limit ?? 10} onChange={(e) => set({ limit: Number(e.target.value) })}>
                  {LIMITS.map((n) => (
                    <option key={n} value={n}>
                      {n} issues
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          )}
        </div>
      )}
      {error ? <ErrorBox error={error} /> : null}
      <div className="-mx-5 mt-1 -mb-4 flex justify-end gap-2 border-t border-line px-5 py-3">
        <Button onClick={onClose}>Cancel</Button>
        <Button variant="primary" type="submit" loading={checking} disabled={meta.scope !== "query" && !c.projectKey}>
          Apply
        </Button>
      </div>
    </form>
  );
}

/** Group labels a control that brings its own accessible name (a picker or the JQL editor). */
function Group({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="flex flex-col gap-1.5">
      <p className="text-[13px] font-medium text-ink">{label}</p>
      {children}
      {hint && <p className="text-xs text-muted">{hint}</p>}
    </div>
  );
}
