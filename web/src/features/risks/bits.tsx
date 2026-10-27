import type { Schemas } from "@/api/client";
import { cn } from "@/lib/cn";
import { IMPACT, LIKELIHOOD, RATING_BANDS, RATING_LABEL, progressLabel, type RiskRating } from "./model";

/** Tint and ink per rating (index.css tokens), so the rating reads in light and dark. */
export const ratingClass: Record<RiskRating, string> = {
  low: "bg-risk-low-soft text-risk-low",
  medium: "bg-risk-medium-soft text-risk-medium",
  high: "bg-risk-high-soft text-risk-high",
  critical: "bg-risk-critical-soft text-risk-critical",
};

/** RatingBadge writes a risk's score and rating ("20 Critical") on the rating's colour. */
export function RatingBadge({ rating, score, className }: { rating?: RiskRating; score?: number; className?: string }) {
  if (!rating) return <span className={cn("text-[13px] text-faint", className)}>Not assessed</span>;
  return (
    <span className={cn("inline-flex h-5 items-center gap-1 rounded px-1.5 text-[11px] font-semibold whitespace-pre", ratingClass[rating], className)}>
      {score != null && <span className="tabular-nums">{score} </span>}
      {RATING_LABEL[rating]}
    </span>
  );
}

/** WorkProgress shows linked work as "2 of 3 done" with a bar. */
export function WorkProgress({ total, done, className }: { total: number; done: number; className?: string }) {
  if (total === 0) return <span className={cn("text-[13px] text-faint", className)}>None</span>;
  return (
    <span className={cn("inline-flex items-center gap-2 text-[13px] whitespace-nowrap", className)}>
      <span aria-hidden className="h-1.5 w-12 overflow-hidden rounded-full bg-surface-3">
        <span className="block h-full rounded-full bg-done" style={{ width: `${Math.round((done / total) * 100)}%` }} />
      </span>
      {progressLabel({ total, done })}
    </span>
  );
}

type Cell = { likelihood: number; impact: number };

/**
 * RiskMatrix is the 5 × 5 heat map of open risks: likelihood down the side (most likely at the
 * top), impact across. Each cell writes its count and score on its rating's colour and names
 * both in full for assistive technology; cells with risks are buttons that filter the register
 * (pressing the selected one clears the filter).
 */
export function RiskMatrix({ cells, selected, onSelect }: { cells: Schemas["RiskCell"][]; selected?: Cell; onSelect: (c: Cell | undefined) => void }) {
  const at = (l: number, i: number) => cells.find((c) => c.likelihood === l && c.impact === i);
  return (
    <figure className="min-w-0">
      <table className="w-full table-fixed border-separate border-spacing-1">
        <caption className="sr-only">Open risks by likelihood (rows) and impact (columns)</caption>
        <thead>
          <tr>
            <td className="w-24" />
            <th scope="colgroup" colSpan={5} className="text-center text-[11px] font-semibold text-muted">
              Impact
            </th>
          </tr>
          <tr>
            <td className="text-left align-bottom text-[11px] font-semibold text-muted">Likelihood</td>
            {IMPACT.map((label, k) => (
              <th key={k} scope="col" className="text-center align-bottom text-[11px] leading-tight font-medium text-muted">
                {k + 1} {label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {[5, 4, 3, 2, 1].map((l) => (
            <tr key={l}>
              <th scope="row" className="pr-1 text-right text-[11px] leading-tight font-medium text-muted">
                {l} {LIKELIHOOD[l - 1]}
              </th>
              {[1, 2, 3, 4, 5].map((i) => {
                const c = at(l, i);
                if (!c) return <td key={i} />;
                const on = selected?.likelihood === l && selected.impact === i;
                const label = `Likelihood ${l} ${LIKELIHOOD[l - 1]}, impact ${i} ${IMPACT[i - 1]}: score ${c.score}, ${RATING_LABEL[c.rating].toLowerCase()}; ${c.count} open risk${c.count === 1 ? "" : "s"}`;
                const body = (
                  <>
                    <span className="absolute top-1 right-1.5 text-[10px] font-medium tabular-nums opacity-90">{c.score}</span>
                    <span className="text-lg leading-none font-semibold tabular-nums">{c.count > 0 ? c.count : ""}</span>
                  </>
                );
                const cls = cn("relative flex h-12 w-full items-center justify-center rounded-md", ratingClass[c.rating]);
                return (
                  <td key={i} className="p-0">
                    {c.count > 0 || on ? (
                      <button
                        type="button"
                        aria-label={label}
                        aria-pressed={on}
                        title={label}
                        onClick={() => onSelect(on ? undefined : { likelihood: l, impact: i })}
                        className={cn(cls, "hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent", on && "ring-2 ring-accent ring-offset-1 ring-offset-surface")}
                      >
                        {body}
                      </button>
                    ) : (
                      <span role="img" aria-label={label} title={label} className={cls}>
                        {body}
                      </span>
                    )}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <figcaption className="mt-2 flex flex-wrap items-center gap-1.5 text-[11px] text-muted">
        <span>Score = likelihood × impact:</span>
        {RATING_BANDS.map((b) => (
          <span key={b.rating} className={cn("rounded px-1.5 py-0.5 font-semibold", ratingClass[b.rating])}>
            {RATING_LABEL[b.rating]} {b.range}
          </span>
        ))}
      </figcaption>
    </figure>
  );
}
