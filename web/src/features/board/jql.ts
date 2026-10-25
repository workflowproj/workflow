/** jqlString quotes a value for JQL: "a \"quoted\" word". */
export function jqlString(v: string) {
  return `"${v.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
}

/** QuickFilters are the board and backlog filters; `people` (assignee ids, see people.ts) applies in the browser. */
export type QuickFilters = { text: string; mine: boolean; recent: boolean; people: string[] };

export const NO_FILTERS: QuickFilters = { text: "", mine: false, recent: false, people: [] };

/** quickFilterJql turns the board's quick filters into JQL that the API ANDs with the board filter. */
export function quickFilterJql(f: Pick<QuickFilters, "text" | "mine" | "recent">): string {
  const parts: string[] = [];
  if (f.mine) parts.push("assignee = currentUser()");
  if (f.recent) parts.push("updated >= -1d");
  const text = f.text.trim();
  if (/^[A-Za-z][A-Za-z0-9_]{1,9}-\d+$/.test(text)) parts.push(`(key = ${text.toUpperCase()} OR text ~ ${jqlString(text)})`);
  else if (text) parts.push(`text ~ ${jqlString(text)}`);
  return parts.join(" AND ");
}

export function hasFilters(f: QuickFilters) {
  return f.mine || f.recent || f.text.trim() !== "" || f.people.length > 0;
}
