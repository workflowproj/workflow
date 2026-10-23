import { useQueries, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import { fieldsQuery, issueTypesQuery, labelsQuery, projectsQuery } from "@/api/queries";
import { releasesQuery, sprintsQuery } from "@/features/issues/issue-api";
import { quoteValue, type Context } from "./jql";
import { searchMetadataQuery } from "./search-api";

export type Suggestion = {
  label: string;
  insert: string;
  detail?: string;
  kind: "field" | "operator" | "value" | "keyword" | "function";
  /** Leave no space after the insertion (values inside a list). */
  tight?: boolean;
};

const norm = (s: string) => s.toLowerCase().replace(/[\s_-]/g, "");

const aliases: Record<string, string> = {
  issuetype: "type",
  label: "labels",
  watchers: "watcher",
  fixversions: "fixversion",
  version: "fixversion",
  release: "fixversion",
  epic: "parent",
  epiclink: "parent",
  points: "storypoints",
  estimate: "storypoints",
  duedate: "due",
  startdate: "start",
  createddate: "created",
  updateddate: "updated",
  resolutiondate: "resolved",
  issuekey: "key",
  issue: "key",
  category: "statuscategory",
  comments: "comment",
  title: "summary",
  hierarchylevel: "level",
};

const ORDER_FIELDS = ["created", "updated", "priority", "key", "summary", "status", "assignee", "storyPoints", "type", "rank", "due", "start", "resolved"];
const RESOLUTIONS = ["Unresolved", "Done", "Won't Do", "Duplicate", "Cannot Reproduce"];
const DATE_VALUES: [string, string][] = [
  ["-1d", "24 hours ago"],
  ["-7d", "a week ago"],
  ["-30d", "30 days ago"],
  ["startOfDay()", "today"],
  ["startOfWeek()", "this week"],
  ["startOfMonth()", "this month"],
  ["endOfMonth()", ""],
  ["now()", ""],
];

const fn = (v: string, detail?: string): Suggestion => ({ label: v, insert: v, kind: "function", detail });
const val = (v: string, detail?: string, label?: string): Suggestion => ({ label: label ?? v, insert: quoteValue(v), kind: "value", detail });
const kw = (label: string, insert = label): Suggestion => ({ label, insert, kind: "keyword" });

/** rank keeps suggestions matching the typed prefix, prefix matches first. */
function rank(items: Suggestion[], prefix: string): Suggestion[] {
  const p = prefix.toLowerCase();
  if (!p) return items;
  const starts = items.filter((s) => s.label.toLowerCase().startsWith(p) || s.insert.toLowerCase().startsWith(p));
  const contains = items.filter((s) => !starts.includes(s) && (s.label.toLowerCase().includes(p) || (s.detail ?? "").toLowerCase().includes(p)));
  const out = [...starts, ...contains];
  // Nothing left to complete when the only match is what was typed.
  if (out.length === 1 && (out[0].label.toLowerCase() === p || out[0].insert.toLowerCase() === p)) return [];
  return out;
}

/**
 * useJqlSuggestions offers completions for the cursor context: field names, operators, values
 * (statuses, people, projects, labels, custom field options…), keywords and sort fields.
 */
export function useJqlSuggestions(ctx: Context | null, projectKey?: string): { items: Suggestion[]; hint?: string } {
  const { data: meta } = useQuery(searchMetadataQuery);
  const { data: projects = [] } = useQuery(projectsQuery);
  const { data: defs = [] } = useQuery(fieldsQuery);

  const expectsValue = ctx?.expect === "value" || ctx?.expect === "list-value";
  const metaField = ctx?.field ? meta?.fields.find((f) => norm(f.name) === (aliases[norm(ctx.field!)] ?? norm(ctx.field!))) : undefined;
  const custom = ctx?.field ? defs.find((d) => norm(d.key) === norm(ctx.field!.replace(/^cf\[(.+)\]$/i, "$1")) || norm(d.name) === norm(ctx.field!)) : undefined;
  const fieldType = metaField?.type ?? custom?.type;
  const keys = projectKey ? [projectKey] : projects.map((p) => p.key);

  const types = useQueries({
    queries: keys.map((k) => ({ ...issueTypesQuery(k), enabled: expectsValue && fieldType === "type" })),
  });
  const labels = useQueries({
    queries: keys.map((k) => ({ ...labelsQuery(k), enabled: expectsValue && fieldType === "label" })),
  });
  const userQ = expectsValue && fieldType === "user" ? ctx!.prefix : "";
  const users = useQuery({
    queryKey: ["users", userQ, projectKey ?? ""],
    queryFn: () => unwrap(api.GET("/users", { params: { query: { q: userQ || undefined, project: projectKey } } })),
    enabled: expectsValue && fieldType === "user",
    staleTime: 30_000,
  });
  const sprints = useQuery({ ...sprintsQuery(projectKey ?? ""), enabled: !!projectKey && expectsValue && fieldType === "sprint" });
  const releases = useQuery({ ...releasesQuery(projectKey ?? ""), enabled: !!projectKey && expectsValue && fieldType === "release" });

  if (!ctx || !meta) return { items: [] };
  const prefix = ctx.prefix;
  const tightValues = ctx.expect === "list-value";

  switch (ctx.expect) {
    case "field": {
      const items: Suggestion[] = meta.fields.map((f) => ({ label: f.name, insert: f.name, kind: "field", detail: defs.find((d) => d.key === f.name)?.name ?? f.type }));
      items.push(kw("NOT"));
      if (ctx.atStart && ctx.from === 0) items.push(kw("ORDER BY", "ORDER BY"));
      return { items: rank(items, prefix).slice(0, 14) };
    }
    case "operator": {
      if (ctx.op === "not") return { items: rank([{ label: "in", insert: "in (", kind: "operator" }], prefix) };
      const ops = metaField?.operators ?? ["=", "!=", "in", "not in", "is", "is not"];
      const items = ops.map<Suggestion>((o) => ({ label: o, insert: o === "in" || o === "not in" ? `${o} (` : o, kind: "operator" }));
      return { items: rank(items, prefix), hint: metaField ? undefined : custom ? undefined : `Unknown field "${ctx.field}"` };
    }
    case "is-value":
      return { items: rank(ctx.op === "is" ? [kw("EMPTY"), kw("NOT EMPTY", "not EMPTY")] : [kw("EMPTY")], prefix) };
    case "logical":
      return { items: rank([kw("AND"), kw("OR"), kw("ORDER BY", "ORDER BY")], prefix) };
    case "by":
      return { items: rank([kw("BY")], prefix) };
    case "order-field":
      return { items: rank(ORDER_FIELDS.map((f) => ({ label: f, insert: f, kind: "field" as const })), prefix) };
    case "order-dir":
      return { items: rank([kw("DESC"), kw("ASC")], prefix) };
    case "value":
    case "list-value": {
      let items: Suggestion[] = [];
      let hint: string | undefined;
      switch (fieldType) {
        case "project":
          items = projects.map((p) => val(p.key, p.name));
          break;
        case "type":
          items = [...new Set(types.flatMap((q) => q.data?.map((t) => t.name) ?? []))].map((n) => val(n));
          break;
        case "status":
        case "category":
        case "priority":
          items = (metaField?.values ?? []).map((v) => val(v));
          break;
        case "resolution":
          items = RESOLUTIONS.map((v) => val(v));
          break;
        case "user":
          items = [fn("currentUser()", "you"), kw("EMPTY"), ...(users.data ?? []).map((u) => val(u.email, u.email, u.displayName))];
          if (custom) items = items.filter((i) => i.label !== "EMPTY");
          break;
        case "label":
          items = [...new Set(labels.flatMap((q) => q.data ?? []))].sort().map((l) => val(l));
          break;
        case "sprint":
          items = [fn("openSprints()"), fn("futureSprints()"), fn("closedSprints()"), kw("EMPTY"), ...(sprints.data ?? []).map((s) => val(s.name, s.state))];
          break;
        case "release":
          items = [fn("unreleasedVersions()"), fn("releasedVersions()"), kw("EMPTY"), ...(releases.data ?? []).map((r) => val(r.name, r.state))];
          break;
        case "issue":
          items = ctx.field && norm(ctx.field) === "parent" ? [kw("EMPTY")] : [];
          hint = "An issue key, e.g. PLAT-12";
          break;
        case "date":
          items = DATE_VALUES.map(([v, d]) => (v.endsWith(")") ? fn(v, d || undefined) : val(v, d)));
          hint = 'A date (2026-01-31), a relative offset (-7d, -2w) or a function';
          break;
        case "select":
        case "multiselect":
          items = (custom?.options ?? []).map((o) => val(o));
          break;
        case "checkbox":
          items = [val("true"), val("false")];
          break;
        case "text":
        case "url":
          hint = 'Type a quoted phrase, e.g. "card declined"';
          break;
        case "number":
          hint = "Type a number";
          break;
      }
      if (tightValues) items = items.map((i) => ({ ...i, tight: true }));
      return { items: rank(items, prefix).slice(0, 14), hint };
    }
    default:
      return { items: [] };
  }
}
