/*
 * Pure helpers for the multi-series charts (cumulative flow, created vs resolved, breakdowns).
 */

/**
 * foldSeries keeps at most `max` series: past that, the tail folds into one "Other" series
 * (key "") so no series ever needs a ninth colour.
 */
export function foldSeries<S extends { key: string; label: string; counts: number[] }>(series: S[], max: number): S[] {
  if (series.length <= max) return series;
  const keep = series.slice(0, max - 1);
  const rest = series.slice(max - 1);
  const counts = rest[0].counts.map((_, i) => rest.reduce((n, s) => n + (s.counts[i] ?? 0), 0));
  return [...keep, { ...rest[rest.length - 1], key: "", label: "Other", counts }];
}

/** stackSeries turns rows of values (bottom first) into each band's lower and upper edges. */
export function stackSeries(rows: number[][]): { lower: number[]; upper: number[] }[] {
  const n = Math.max(0, ...rows.map((r) => r.length));
  let base = Array.from({ length: n }, () => 0);
  return rows.map((r) => {
    const upper = base.map((b, i) => b + (r[i] ?? 0));
    const band = { lower: base, upper };
    base = upper;
    return band;
  });
}

export type Group = { key: string; label: string; value: number };

/**
 * foldGroups keeps the largest `max` groups for a breakdown. The rest fold into "Other"; the
 * "None" group (key "") keeps its own row at the end because it usually needs attention.
 */
export function foldGroups<G extends Group>(groups: G[], max: number): (G | (Group & { other: true; count: number }))[] {
  const none = groups.filter((g) => g.key === "");
  const named = groups.filter((g) => g.key !== "");
  const room = Math.max(1, max - none.length);
  if (named.length <= room) return [...named, ...none];
  const keep = named.slice(0, room - 1);
  const rest = named.slice(room - 1);
  const other = { key: "\u0000other", label: `${rest.length} more`, value: rest.reduce((n, g) => n + g.value, 0), other: true as const, count: rest.length };
  return [...keep, other, ...none];
}

/** arcPath draws a donut segment between two angles (radians, 0 at 12 o'clock, clockwise). */
export function arcPath(cx: number, cy: number, r: number, inner: number, a0: number, a1: number): string {
  if (a1 - a0 >= Math.PI * 2 - 1e-6) {
    // A full ring: two half arcs, since one arc can't start and end at the same point.
    const mid = a0 + Math.PI;
    return `${arcPath(cx, cy, r, inner, a0, mid)}${arcPath(cx, cy, r, inner, mid, a0 + Math.PI * 2)}`;
  }
  const pt = (rad: number, a: number) => `${cx + rad * Math.sin(a)},${cy - rad * Math.cos(a)}`;
  const large = a1 - a0 > Math.PI ? 1 : 0;
  return `M${pt(r, a0)}A${r},${r} 0 ${large} 1 ${pt(r, a1)}L${pt(inner, a1)}A${inner},${inner} 0 ${large} 0 ${pt(inner, a0)}Z`;
}
