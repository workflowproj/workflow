/**
 * linkIssueKeys turns bare issue keys (ABC-123) in Markdown into links to the issue, leaving code
 * spans, code blocks and existing links alone. Only keys of `projectKeys` link (the projects the
 * viewer can browse, with their earlier keys), so look-alikes such as "DS-2026" or "ISO-9001"
 * stay text.
 */
export function linkIssueKeys(md: string, projectKeys: ReadonlySet<string>): string {
  if (projectKeys.size === 0) return md;
  return md
    .split(/(```[\s\S]*?```|`[^`]*`|\[[^\]]*\]\([^)]*\))/g)
    .map((part, i) => (i % 2 === 1 ? part : part.replace(/(^|[^\w/-])(([A-Z][A-Z0-9_]{1,9})-\d+)\b/g, (m, pre: string, key: string, project: string) => (projectKeys.has(project) ? `${pre}[${key}](/browse/${key})` : m))))
    .join("");
}

/** projectKeySet is every key that names one of the projects, current keys and earlier ones alike. */
export function projectKeySet(projects: readonly { key: string; keyAliases?: string[] }[] | undefined): Set<string> {
  return new Set((projects ?? []).flatMap((p) => [p.key, ...(p.keyAliases ?? [])]));
}
