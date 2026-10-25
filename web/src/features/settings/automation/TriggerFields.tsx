import { Field, Input, Select } from "@/components/ui";
import { cn } from "@/lib/cn";
import { CATEGORIES, CHANGEABLE_FIELDS, CRON_PRESETS, joinFields, splitFields, TRIGGERS, type Category, type Trigger } from "./meta";

/** TriggerFields edits the "When" part of a rule. */
export function TriggerFields({
  trigger,
  onChange,
  statuses,
  customFields,
  errors,
  projectKey,
  missing,
}: {
  trigger: Trigger;
  onChange: (t: Trigger) => void;
  statuses: string[];
  customFields: { key: string; name: string }[];
  errors: Record<string, string>;
  projectKey: string;
  missing: Set<string>;
}) {
  const entering = trigger.toCategory ? `cat:${trigger.toCategory}` : trigger.toStatus ? `status:${trigger.toStatus}` : "";
  const fields = splitFields(trigger.field);
  const toggleField = (k: string) =>
    onChange({ ...trigger, field: joinFields(fields.some((f) => f.toLowerCase() === k.toLowerCase()) ? fields.filter((f) => f.toLowerCase() !== k.toLowerCase()) : [...fields, k]) });
  const allFields = [...CHANGEABLE_FIELDS, ...customFields.map((f) => ({ key: f.key, label: f.name }))];

  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label="Trigger" error={errors["trigger.type"]}>
        {(id) => (
          <Select id={id} value={trigger.type} onChange={(e) => onChange({ type: e.target.value as Trigger["type"] })}>
            {(["Issues", "Development", "Schedule"] as const).map((g) => (
              <optgroup key={g} label={g}>
                {TRIGGERS.filter((t) => t.group === g).map((t) => (
                  <option key={t.type} value={t.type}>
                    {t.label}
                  </option>
                ))}
              </optgroup>
            ))}
          </Select>
        )}
      </Field>

      {trigger.type === "issue.transitioned" && (
        <Field
          label="Entering"
          error={errors["trigger.toCategory"] ?? (trigger.toStatus && missing.has(trigger.toStatus.toLowerCase()) ? `“${trigger.toStatus}” is not a status in this project's workflows` : undefined)}
          hint={trigger.toCategory ? "Any status of this category, in every workflow — robust when workflows differ." : undefined}
        >
          {(id) => (
            <Select
              id={id}
              value={entering}
              onChange={(e) => {
                const v = e.target.value;
                onChange({ type: trigger.type, ...(v.startsWith("cat:") ? { toCategory: v.slice(4) as Category } : v.startsWith("status:") ? { toStatus: v.slice(7) } : {}) });
              }}
            >
              <option value="">Any status change</option>
              <optgroup label="Any status of a category">
                {CATEGORIES.map((c) => (
                  <option key={c.id} value={`cat:${c.id}`}>
                    Any {c.label} status
                  </option>
                ))}
              </optgroup>
              <optgroup label="A specific status">
                {trigger.toStatus && !statuses.some((s) => s.toLowerCase() === trigger.toStatus!.toLowerCase()) && (
                  <option value={`status:${trigger.toStatus}`}>{trigger.toStatus} (not in this project)</option>
                )}
                {statuses.map((s) => (
                  <option key={s} value={`status:${s}`}>
                    {s}
                  </option>
                ))}
              </optgroup>
            </Select>
          )}
        </Field>
      )}

      {trigger.type === "issue.field_changed" && (
        <fieldset className="sm:col-span-2">
          <legend className="mb-1.5 text-[13px] font-medium">Fields to watch</legend>
          <div className="flex flex-wrap gap-1.5">
            {allFields.map((f) => {
              const on = fields.some((x) => x.toLowerCase() === f.key.toLowerCase());
              return (
                <button
                  key={f.key}
                  type="button"
                  aria-pressed={on}
                  onClick={() => toggleField(f.key)}
                  className={cn(
                    "rounded-full border px-2.5 py-0.5 text-[13px]",
                    on ? "border-accent bg-accent-soft font-medium text-accent" : "border-line text-muted hover:border-line-strong hover:text-ink",
                  )}
                >
                  {f.label}
                </button>
              );
            })}
          </div>
          <p className="mt-1 text-xs text-muted">{fields.length ? "The rule runs when any of these change." : "None selected: the rule runs on any edit."}</p>
        </fieldset>
      )}

      {trigger.type === "schedule" && (
        <>
          <Field label="Cron (UTC)" error={errors["trigger.cron"]} hint="minute hour day month weekday, or @daily / @hourly">
            {(id) => (
              <div className="flex gap-2">
                <Input
                  id={id}
                  value={trigger.cron ?? ""}
                  onChange={(e) => onChange({ ...trigger, cron: e.target.value })}
                  className="font-mono"
                  placeholder="0 9 * * 1-5"
                  invalid={!!errors["trigger.cron"]}
                />
                <Select aria-label="Cron presets" value="" onChange={(e) => e.target.value && onChange({ ...trigger, cron: e.target.value })} className="w-32 shrink-0">
                  <option value="">Presets…</option>
                  {CRON_PRESETS.map((p) => (
                    <option key={p.cron} value={p.cron}>
                      {p.label}
                    </option>
                  ))}
                </Select>
              </div>
            )}
          </Field>
          <Field label="Issues (query)" error={errors["trigger.jql"]} hint="Searched within this project; the actions run once for each match." className="sm:col-span-2">
            {(id) => (
              <Input
                id={id}
                value={trigger.jql ?? ""}
                onChange={(e) => onChange({ ...trigger, jql: e.target.value })}
                className="font-mono text-[13px]"
                placeholder={`project = ${projectKey} AND status = "In Review" AND updated < -7d`}
                invalid={!!errors["trigger.jql"]}
              />
            )}
          </Field>
        </>
      )}
    </div>
  );
}
