import { ArrowRight } from "lucide-react";
import { Select } from "@/components/ui";
import { StatusBadge } from "@/components/issue-bits";
import type { StatusCategory } from "@/api/types";

export type RemovedStatus = { name: string; issueCount?: number; message?: string };

/**
 * StatusMappingFields asks where issues sitting in removed statuses should go. `value` maps
 * old status names to new status names, as the API's `statusMapping` expects.
 */
export function StatusMappingFields({
  removed,
  targets,
  value,
  onChange,
  oldCategory,
}: {
  removed: RemovedStatus[];
  targets: { name: string; category: StatusCategory }[];
  value: Record<string, string>;
  onChange: (v: Record<string, string>) => void;
  oldCategory?: (name: string) => StatusCategory | undefined;
}) {
  return (
    <div className="overflow-hidden rounded-md border border-line">
      <div className="grid grid-cols-[minmax(0,1fr)_1.5rem_minmax(0,1fr)] items-center gap-2 border-b border-line bg-surface-2 px-3 py-1.5 text-xs font-semibold text-muted">
        <span>Issues currently in</span>
        <span />
        <span>Move them to</span>
      </div>
      {removed.map((r) => {
        const id = `map-${r.name}`;
        return (
          <div key={r.name} className="grid grid-cols-[minmax(0,1fr)_1.5rem_minmax(0,1fr)] items-center gap-2 border-b border-line px-3 py-2 last:border-b-0">
            <div className="min-w-0">
              <StatusBadge status={{ name: r.name, category: oldCategory?.(r.name) ?? "todo" }} />
              <p className="mt-0.5 text-xs text-muted">{r.issueCount !== undefined ? `${r.issueCount} issue${r.issueCount === 1 ? "" : "s"}` : r.message}</p>
            </div>
            <ArrowRight className="size-4 text-faint" aria-hidden />
            <Select id={id} aria-label={`New status for issues in ${r.name}`} value={value[r.name] ?? ""} onChange={(e) => onChange({ ...value, [r.name]: e.target.value })} invalid={!value[r.name]}>
              <option value="">Choose a status…</option>
              {targets.map((t) => (
                <option key={t.name} value={t.name}>
                  {t.name}
                </option>
              ))}
            </Select>
          </div>
        );
      })}
    </div>
  );
}

/** suggestMapping pre-selects a target with the same name or, failing that, the same category. */
export function suggestMapping(
  removed: RemovedStatus[],
  targets: { name: string; category: StatusCategory }[],
  categoryOf: (name: string) => StatusCategory | undefined,
  current: Record<string, string> = {},
  renames: Record<string, string> = {},
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of removed) {
    if (current[r.name] && targets.some((t) => t.name === current[r.name])) {
      out[r.name] = current[r.name];
      continue;
    }
    const renamed = renames[r.name];
    const byName = targets.find((t) => t.name.toLowerCase() === (renamed ?? r.name).toLowerCase());
    const cat = categoryOf(r.name);
    const byCat = cat ? targets.find((t) => t.category === cat) : undefined;
    const pick = byName ?? byCat;
    if (pick) out[r.name] = pick.name;
  }
  return out;
}
