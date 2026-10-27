import { useState } from "react";
import { getRouteApi } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, CalendarClock, Plus, Trash2, X } from "lucide-react";
import { api, ApiError, unwrap } from "@/api/client";
import type { Priority } from "@/api/types";
import { Button, Checkbox, IconButton, Input, PageHeader, PageSpinner, Select, useToast } from "@/components/ui";
import { ErrorBox } from "@/components/ErrorBox";
import { cn } from "@/lib/cn";
import { Callout, SettingsSection, Switch } from "@/features/settings/common";
import { useProjectStatuses } from "@/features/settings/queries";
import { requestTypesQuery, slaConfigQuery, type SlaCalendar, type SlaConfig, type SlaGoal } from "@/features/servicedesk/api";
import { formatSeconds } from "@/features/servicedesk/sla";

const route = getRouteApi("/app/projects/$projectKey/settings/slas");

const PRIORITIES: Priority[] = ["highest", "high", "medium", "low", "lowest"];
// Monday first; 0 = Sunday as in the API.
const DAYS = [
  [1, "Monday"],
  [2, "Tuesday"],
  [3, "Wednesday"],
  [4, "Thursday"],
  [5, "Friday"],
  [6, "Saturday"],
  [0, "Sunday"],
] as const;

function timeZones(): string[] {
  const intl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] };
  try {
    return intl.supportedValuesOf?.("timeZone") ?? [];
  } catch {
    return [];
  }
}

/**
 * SlaSettings edits a desk's SLAs as one document: the calendars (working hours) and, for time to
 * first response and time to resolution, the statuses that pause the clock and the goals (the
 * first goal whose conditions match a request applies).
 */
export function SlaSettings() {
  const { projectKey } = route.useParams();
  const cfg = useQuery(slaConfigQuery(projectKey));
  return (
    <div className="flex max-w-4xl flex-col gap-5">
      <PageHeader title="SLAs" subtitle="How quickly requests should get a first reply and be resolved, counted in working time." />
      {cfg.error && <ErrorBox error={cfg.error} />}
      {cfg.isLoading ? <PageSpinner /> : cfg.data && <SlaEditor key={projectKey} projectKey={projectKey} initial={cfg.data} />}
    </div>
  );
}

function SlaEditor({ projectKey, initial }: { projectKey: string; initial: SlaConfig }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [c, setC] = useState<SlaConfig>(() => structuredClone(initial));
  const dirty = JSON.stringify(c) !== JSON.stringify(initial);
  const save = useMutation({
    mutationFn: () => unwrap(api.PUT("/projects/{projectKey}/slas", { params: { path: { projectKey } }, body: c })),
    onSuccess: (data) => {
      qc.setQueryData(["slas", projectKey], data);
      // Continue from what was saved (calendars get their ids); refetches keep unsaved edits.
      setC(structuredClone(data));
      // Clocks are recomputed in the background; refresh what shows them shortly after.
      setTimeout(() => {
        qc.invalidateQueries({ queryKey: ["queues", projectKey] });
        qc.invalidateQueries({ queryKey: ["queue"] });
      }, 1500);
      toast.success("SLAs saved; open requests are being recalculated");
    },
  });
  const errs = save.error instanceof ApiError ? save.error.fieldErrors() : {};
  const setCalendar = (i: number, next: SlaCalendar) =>
    setC((cur) => {
      const old = cur.calendars[i].name;
      return {
        calendars: cur.calendars.map((x, j) => (j === i ? next : x)),
        // Renaming a calendar keeps the goals that use it.
        metrics: next.name === old ? cur.metrics : cur.metrics.map((m) => ({ ...m, goals: m.goals.map((g) => (g.calendar === old ? { ...g, calendar: next.name } : g)) })),
      };
    });
  const removeCalendar = (i: number) =>
    setC((cur) => {
      const name = cur.calendars[i].name;
      return {
        calendars: cur.calendars.filter((_, j) => j !== i),
        metrics: cur.metrics.map((m) => ({ ...m, goals: m.goals.map((g) => (g.calendar === name ? { ...g, calendar: undefined } : g)) })),
      };
    });
  return (
    <form
      className="flex flex-col gap-5"
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
    >
      <SettingsSection
        title="Calendars"
        description="Working time the clocks count. A goal without a calendar counts around the clock."
        actions={
          <Button
            size="sm"
            icon={<Plus className="size-3.5" />}
            onClick={() =>
              setC((cur) => ({
                ...cur,
                calendars: [
                  ...cur.calendars,
                  { name: uniqueName(cur.calendars), timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC", hours: defaultHours(), holidays: [] },
                ],
              }))
            }
          >
            Add calendar
          </Button>
        }
      >
        {c.calendars.length === 0 ? (
          <p className="px-4 py-3 text-[13px] text-muted">No calendars: every goal counts around the clock.</p>
        ) : (
          <div className="flex flex-col divide-y divide-line">
            {c.calendars.map((cal, i) => (
              <CalendarEditor key={cal.id ?? `new-${i}`} index={i} calendar={cal} errors={errs} onChange={(next) => setCalendar(i, next)} onRemove={() => removeCalendar(i)} />
            ))}
          </div>
        )}
      </SettingsSection>
      {c.metrics.map((m, mi) => (
        <MetricEditor
          key={m.metric}
          projectKey={projectKey}
          index={mi}
          metric={m}
          calendars={c.calendars.map((x) => x.name)}
          errors={errs}
          onChange={(next) => setC((cur) => ({ ...cur, metrics: cur.metrics.map((x, j) => (j === mi ? next : x)) }))}
        />
      ))}
      {save.error ? <ErrorBox error={save.error} /> : null}
      <div className="sticky bottom-0 -mx-1 flex items-center gap-2 border-t border-line bg-canvas/95 px-1 py-3 backdrop-blur">
        <Button type="submit" variant="primary" loading={save.isPending} disabled={!dirty}>
          Save SLAs
        </Button>
        <Button variant="ghost" disabled={!dirty || save.isPending} onClick={() => (setC(structuredClone(initial)), save.reset())}>
          Discard changes
        </Button>
        {dirty && <span className="text-[13px] text-muted">Unsaved changes</span>}
      </div>
    </form>
  );
}

function uniqueName(cals: SlaCalendar[]) {
  for (let n = 1; ; n++) {
    const name = n === 1 ? "Working hours" : `Working hours ${n}`;
    if (!cals.some((c) => c.name === name)) return name;
  }
}

function defaultHours(): SlaCalendar["hours"] {
  return [1, 2, 3, 4, 5].map((day) => ({ day, start: "09:00", end: "17:00" }));
}

const timeRe = /^([01]\d|2[0-3]):[0-5]\d$|^24:00$/;

function CalendarEditor({
  index,
  calendar: cal,
  errors,
  onChange,
  onRemove,
}: {
  index: number;
  calendar: SlaCalendar;
  errors: Record<string, string>;
  onChange: (c: SlaCalendar) => void;
  onRemove: () => void;
}) {
  const [holiday, setHoliday] = useState("");
  const zones = timeZones();
  const allDay = cal.hours.length === 0;
  const where = `calendars[${index}]`;
  const err = Object.entries(errors).find(([k]) => k === where || k.startsWith(where + "."))?.[1];
  const setDay = (day: number, intervals: { start: string; end: string }[]) =>
    onChange({
      ...cal,
      hours: [...cal.hours.filter((h) => h.day !== day), ...intervals.map((x) => ({ day, ...x }))].sort(
        (a, b) => ((a.day + 6) % 7) - ((b.day + 6) % 7) || a.start.localeCompare(b.start),
      ),
    });
  return (
    <fieldset className="flex flex-col gap-4 p-4">
      <legend className="sr-only">{cal.name || "Calendar"}</legend>
      <div className="flex flex-wrap items-end gap-3">
        <label className="flex min-w-48 flex-1 flex-col gap-1.5">
          <span className="text-[13px] font-medium">Name</span>
          <Input value={cal.name} maxLength={80} onChange={(e) => onChange({ ...cal, name: e.target.value })} />
        </label>
        <label className="flex min-w-48 flex-1 flex-col gap-1.5">
          <span className="text-[13px] font-medium">Time zone</span>
          <Input value={cal.timeZone} list="sla-time-zones" onChange={(e) => onChange({ ...cal, timeZone: e.target.value })} placeholder="Europe/London" />
        </label>
        {index === 0 && (
          <datalist id="sla-time-zones">
            {zones.map((z) => (
              <option key={z} value={z} />
            ))}
          </datalist>
        )}
        <IconButton label={`Delete calendar ${cal.name}`} onClick={onRemove}>
          <Trash2 className="size-4" />
        </IconButton>
      </div>
      <Switch checked={allDay} onChange={(on) => onChange({ ...cal, hours: on ? [] : defaultHours() })} label="Around the clock, every day" />
      {!allDay && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-medium">Working hours</span>
          <ul className="flex flex-col gap-1">
            {DAYS.map(([day, label]) => {
              const intervals = cal.hours.filter((h) => h.day === day);
              const on = intervals.length > 0;
              return (
                <li key={day} className="flex min-h-8.5 flex-wrap items-center gap-x-3 gap-y-1">
                  <Checkbox className="w-32" label={label} checked={on} onChange={(e) => setDay(day, e.target.checked ? [{ start: "09:00", end: "17:00" }] : [])} />
                  {on ? (
                    <span className="flex flex-wrap items-center gap-2">
                      {intervals.map((h, k) => (
                        <span key={k} className="inline-flex items-center gap-1">
                          <TimeInput
                            label={`${label} from`}
                            value={h.start}
                            onChange={(v) =>
                              setDay(
                                day,
                                intervals.map((x, j) => (j === k ? { start: v, end: x.end } : x)),
                              )
                            }
                          />
                          <span className="text-faint">–</span>
                          <TimeInput
                            label={`${label} until`}
                            value={h.end}
                            onChange={(v) =>
                              setDay(
                                day,
                                intervals.map((x, j) => (j === k ? { start: x.start, end: v } : x)),
                              )
                            }
                          />
                          {intervals.length > 1 && (
                            <IconButton
                              size="sm"
                              label={`Remove ${label} hours ${h.start}–${h.end}`}
                              onClick={() =>
                                setDay(
                                  day,
                                  intervals.filter((_, j) => j !== k),
                                )
                              }
                            >
                              <X className="size-3.5" />
                            </IconButton>
                          )}
                        </span>
                      ))}
                      <button type="button" className="text-xs text-accent hover:underline" onClick={() => setDay(day, [...intervals, { start: "18:00", end: "20:00" }])}>
                        Split day
                      </button>
                    </span>
                  ) : (
                    <span className="text-[13px] text-faint">Closed</span>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-1.5">
        <span className="text-[13px] font-medium">Days off</span>
        <div className="flex flex-wrap items-center gap-1.5">
          {cal.holidays.map((d) => (
            <span key={d} className="inline-flex items-center gap-1 rounded-full border border-line bg-surface-2 py-0.5 pr-0.5 pl-2.5 text-[13px] tabular-nums">
              {d}
              <IconButton size="sm" className="size-5" label={`Remove day off ${d}`} onClick={() => onChange({ ...cal, holidays: cal.holidays.filter((x) => x !== d) })}>
                <X className="size-3" />
              </IconButton>
            </span>
          ))}
          <span className="inline-flex items-center gap-1.5">
            <Input type="date" aria-label={`Add a day off to ${cal.name}`} className="w-40" value={holiday} onChange={(e) => setHoliday(e.target.value)} />
            <Button
              size="sm"
              disabled={!holiday || cal.holidays.includes(holiday)}
              onClick={() => {
                onChange({ ...cal, holidays: [...cal.holidays, holiday].sort() });
                setHoliday("");
              }}
            >
              Add
            </Button>
          </span>
        </div>
      </div>
      {err && <p className="text-xs text-danger">{err}</p>}
    </fieldset>
  );
}

function TimeInput({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  const bad = !timeRe.test(value);
  return (
    <Input
      aria-label={label}
      value={value}
      invalid={bad}
      inputMode="numeric"
      placeholder="09:00"
      maxLength={5}
      className="w-18 text-center tabular-nums"
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

type MetricConfig = SlaConfig["metrics"][number];

function MetricEditor({
  projectKey,
  index,
  metric: m,
  calendars,
  errors,
  onChange,
}: {
  projectKey: string;
  index: number;
  metric: MetricConfig;
  calendars: string[];
  errors: Record<string, string>;
  onChange: (m: MetricConfig) => void;
}) {
  const { statuses } = useProjectStatuses(projectKey);
  const { data: types = [] } = useQuery(requestTypesQuery(projectKey));
  const where = `metrics[${index}]`;
  const setGoal = (i: number, g: SlaGoal) => onChange({ ...m, goals: m.goals.map((x, j) => (j === i ? g : x)) });
  const moveGoal = (i: number, d: number) => {
    const goals = [...m.goals];
    [goals[i], goals[i + d]] = [goals[i + d], goals[i]];
    onChange({ ...m, goals });
  };
  const stops = m.metric === "first_response" ? "stops at the team's first reply to the customer" : "stops when the request is resolved (reaches a done status)";
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-1.5">
          <CalendarClock aria-hidden className="size-4 text-muted" /> {m.name}
        </span>
      }
      description={`Starts when the request is raised and ${stops}.`}
    >
      <div className="flex flex-col gap-5 p-4">
        <label className="flex max-w-sm flex-col gap-1.5">
          <span className="text-[13px] font-medium">Name</span>
          <Input value={m.name} maxLength={80} onChange={(e) => onChange({ ...m, name: e.target.value })} />
        </label>
        <fieldset className="flex flex-col gap-1.5">
          <legend className="mb-1 text-[13px] font-medium">Pause while the request is in</legend>
          <div className="flex flex-wrap gap-x-4 gap-y-1.5">
            {statuses
              .filter((s) => s.category !== "done")
              .map((s) => (
                <Checkbox
                  key={s.id}
                  label={s.name}
                  checked={m.pauseStatuses.includes(s.name)}
                  onChange={(e) => onChange({ ...m, pauseStatuses: e.target.checked ? [...m.pauseStatuses, s.name] : m.pauseStatuses.filter((x) => x !== s.name) })}
                />
              ))}
          </div>
        </fieldset>
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-[13px] font-medium">Goals</h3>
            <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => onChange({ ...m, goals: [...m.goals, { targetMinutes: 480, calendar: calendars[0] }] })}>
              Add goal
            </Button>
          </div>
          <p className="text-[13px] text-muted">The first goal whose conditions match a request applies; requests that match no goal are not measured for {m.name.toLowerCase()}.</p>
          {m.goals.length === 0 && (
            <Callout tone="warning" title="No goals">
              No request is measured.
            </Callout>
          )}
          <ol className="flex flex-col gap-2">
            {m.goals.map((g, i) => (
              <GoalEditor
                key={i}
                goal={g}
                index={i}
                last={i === m.goals.length - 1}
                calendars={calendars}
                types={types}
                error={Object.entries(errors).find(([k]) => k.startsWith(`${where}.goals[${i}]`))?.[1]}
                onChange={(next) => setGoal(i, next)}
                onMove={(d) => moveGoal(i, d)}
                onRemove={() => onChange({ ...m, goals: m.goals.filter((_, j) => j !== i) })}
              />
            ))}
          </ol>
        </div>
      </div>
    </SettingsSection>
  );
}

function GoalEditor({
  goal: g,
  index,
  last,
  calendars,
  types,
  error,
  onChange,
  onMove,
  onRemove,
}: {
  goal: SlaGoal;
  index: number;
  last: boolean;
  calendars: string[];
  types: { id: string; name: string }[];
  error?: string;
  onChange: (g: SlaGoal) => void;
  onMove: (d: number) => void;
  onRemove: () => void;
}) {
  const priorities = g.priorities ?? [];
  const typeIds = g.requestTypeIds ?? [];
  const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  const label = `Goal ${index + 1}`;
  return (
    <li className="rounded-md border border-line bg-surface-2/40 p-3">
      <div className="mb-2 flex items-center gap-2">
        <span className="text-[13px] font-semibold">{label}</span>
        <span className="text-xs text-muted">{formatSeconds(g.targetMinutes * 60)}</span>
        <span className="ml-auto flex">
          <IconButton size="sm" label={`Move ${label} up`} disabled={index === 0} onClick={() => onMove(-1)}>
            <ArrowUp className="size-4" />
          </IconButton>
          <IconButton size="sm" label={`Move ${label} down`} disabled={last} onClick={() => onMove(1)}>
            <ArrowDown className="size-4" />
          </IconButton>
          <IconButton size="sm" label={`Delete ${label}`} onClick={onRemove}>
            <Trash2 className="size-4" />
          </IconButton>
        </span>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-xs font-medium text-muted">Priorities (none ticked = any)</legend>
          <div className="flex flex-wrap gap-1">
            {PRIORITIES.map((p) => (
              <Chip key={p} pressed={priorities.includes(p)} onClick={() => onChange({ ...g, priorities: toggle(priorities, p) })}>
                {p.charAt(0).toUpperCase() + p.slice(1)}
              </Chip>
            ))}
          </div>
        </fieldset>
        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-xs font-medium text-muted">Request types (none ticked = any)</legend>
          <div className="flex flex-wrap gap-1">
            {types.map((t) => (
              <Chip key={t.id} pressed={typeIds.includes(t.id)} onClick={() => onChange({ ...g, requestTypeIds: toggle(typeIds, t.id) })}>
                {t.name}
              </Chip>
            ))}
          </div>
        </fieldset>
        <label className="flex flex-col gap-1 md:col-span-2">
          <span className="text-xs font-medium text-muted">And matching a query (optional)</span>
          <Input
            value={g.jql ?? ""}
            placeholder='e.g. labels = vip OR "Request type" = "Report a problem"'
            className="font-mono text-[13px]"
            spellCheck={false}
            onChange={(e) => onChange({ ...g, jql: e.target.value || undefined })}
          />
        </label>
        <TargetInput label={`${label} target`} minutes={g.targetMinutes} onChange={(targetMinutes) => onChange({ ...g, targetMinutes })} />
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted">Counted in</span>
          <Select value={g.calendar ?? ""} onChange={(e) => onChange({ ...g, calendar: e.target.value || undefined })}>
            <option value="">Around the clock</option>
            {calendars.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </Select>
        </label>
      </div>
      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
    </li>
  );
}

function Chip({ pressed, onClick, children }: { pressed: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "h-7 rounded-full border px-2.5 text-[13px] focus-visible:outline-2 focus-visible:outline-accent",
        pressed ? "border-accent bg-accent-soft font-medium text-accent" : "border-line-strong bg-surface text-muted hover:text-ink",
      )}
    >
      {children}
    </button>
  );
}

function TargetInput({ label, minutes, onChange }: { label: string; minutes: number; onChange: (m: number) => void }) {
  const [unit, setUnit] = useState<"minutes" | "hours">(minutes % 60 === 0 ? "hours" : "minutes");
  const factor = unit === "hours" ? 60 : 1;
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted">Goal</span>
      <span className="flex gap-2">
        <Input
          type="number"
          min={1}
          aria-label={label}
          className="w-24"
          value={String(Math.round((minutes / factor) * 100) / 100)}
          onChange={(e) => {
            const n = Number(e.target.value);
            if (n > 0) onChange(Math.max(1, Math.round(n * factor)));
          }}
        />
        <Select aria-label={`${label} unit`} className="w-32" value={unit} onChange={(e) => setUnit(e.target.value as "minutes" | "hours")}>
          <option value="minutes">minutes</option>
          <option value="hours">hours</option>
        </Select>
      </span>
    </div>
  );
}
