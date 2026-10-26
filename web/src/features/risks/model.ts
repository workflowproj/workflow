import type { Schemas } from "@/api/client";

/*
 * Risks and decisions: the project's risk register and decision log are issues of special
 * issue-type kinds, kept out of planning. Their fields have reserved keys (the API's), so search,
 * the matrix and the API agree. Pure helpers here; the pages and panels sit next to this file.
 */

export type IssueTypeKind = Schemas["IssueTypeKind"];
export type RiskRating = Schemas["RiskRating"];
export type LinkType = Schemas["LinkType"];
type IssueLink = Schemas["IssueLink"];

export const RISK_FIELDS = {
  likelihood: "risk_likelihood",
  impact: "risk_impact",
  score: "risk_score",
  rating: "risk_rating",
  response: "risk_response",
  mitigation: "risk_mitigation",
} as const;

export const DECISION_FIELDS = {
  context: "decision_context",
  options: "decision_options",
  outcome: "decision_outcome",
  consequences: "decision_consequences",
  date: "decision_date",
} as const;

/** The fields the risk and decision panels show (the generic custom-field list leaves them out). */
export const RESERVED_FIELDS: ReadonlySet<string> = new Set([...Object.values(RISK_FIELDS), ...Object.values(DECISION_FIELDS)]);

/** Likelihood and impact run from 1 to 5; index 0 is 1. */
export const LIKELIHOOD = ["Rare", "Unlikely", "Possible", "Likely", "Almost certain"];
export const IMPACT = ["Negligible", "Minor", "Moderate", "Major", "Severe"];
export const RESPONSES = ["Avoid", "Reduce", "Transfer", "Accept"] as const;

export const RATING_LABEL: Record<RiskRating, string> = { low: "Low", medium: "Medium", high: "High", critical: "Critical" };

/** The score bands, as the server rates them (no score falls between 16 and 20). */
export const RATING_BANDS: { rating: RiskRating; range: string }[] = [
  { rating: "low", range: "1–4" },
  { rating: "medium", range: "5–9" },
  { rating: "high", range: "10–16" },
  { rating: "critical", range: "20–25" },
];

export function riskRating(score: number): RiskRating {
  if (score >= 20) return "critical";
  if (score >= 10) return "high";
  if (score >= 5) return "medium";
  return "low";
}

/** levelOf reads a likelihood or impact: a whole number from 1 to 5. */
export function levelOf(v: unknown): number | undefined {
  return typeof v === "number" && Number.isInteger(v) && v >= 1 && v <= 5 ? v : undefined;
}

export const likelihoodLabel = (n: number) => `${n} · ${LIKELIHOOD[n - 1]}`;
export const impactLabel = (n: number) => `${n} · ${IMPACT[n - 1]}`;

export type RiskAssessment = { likelihood?: number; impact?: number; score?: number; rating?: RiskRating; response?: string; mitigation?: string };

/** assessRisk reads a risk's fields; the score and rating follow from the likelihood and impact. */
export function assessRisk(fields: Record<string, unknown>): RiskAssessment {
  const likelihood = levelOf(fields[RISK_FIELDS.likelihood]);
  const impact = levelOf(fields[RISK_FIELDS.impact]);
  const out: RiskAssessment = { likelihood, impact };
  if (likelihood && impact) {
    out.score = likelihood * impact;
    out.rating = riskRating(out.score);
  }
  const response = fields[RISK_FIELDS.response];
  if (typeof response === "string" && response) out.response = response;
  const mitigation = fields[RISK_FIELDS.mitigation];
  if (typeof mitigation === "string" && mitigation.trim()) out.mitigation = mitigation;
  return out;
}

/** textField reads a text field ("" when unset). */
export const textField = (fields: Record<string, unknown>, key: string) => (typeof fields[key] === "string" ? (fields[key] as string) : "");

/**
 * linkedFrom lists the issues linked to this one with a link type, from the other side (the work
 * mitigating a risk, implementing or superseding a decision), and how many are done.
 */
export function linkedFrom(links: IssueLink[], type: LinkType) {
  const matching = links.filter((l) => l.type === type && l.direction === "inward");
  const issues = matching.map((l) => l.issue);
  return { links: matching, issues, total: issues.length, done: issues.filter((i) => i.status.category === "done").length };
}

/** linkedTo lists the issues this one links to with a type (the decisions a decision supersedes). */
export function linkedTo(links: IssueLink[], type: LinkType) {
  return links.filter((l) => l.type === type && l.direction === "outward").map((l) => l.issue);
}

/** isOverdue: an open risk whose review date (due date) has passed. */
export function isOverdue(dueDate: string | undefined, category: Schemas["StatusCategory"], today: string) {
  return !!dueDate && category !== "done" && dueDate < today;
}

/** progressLabel renders "2 of 3 done", or "None" without linked work. */
export function progressLabel(p: { total: number; done: number }) {
  return p.total === 0 ? "None" : `${p.done} of ${p.total} done`;
}

type DecisionForExport = {
  key: string;
  summary: string;
  status: { name: string };
  fields: Record<string, unknown>;
  links: IssueLink[];
};

/**
 * adrMarkdown writes a decision as an architecture decision record: title, status (with the
 * date and the decisions it supersedes or that supersede it), context, options, decision and
 * consequences.
 */
export function adrMarkdown(d: DecisionForExport): string {
  const status = [`- Status: ${d.status.name}`];
  const date = textField(d.fields, DECISION_FIELDS.date);
  if (date) status.push(`- Date: ${date}`);
  const ref = (i: { key: string; summary: string }) => `${i.key} (${i.summary})`;
  const supersededBy = linkedFrom(d.links, "supersedes").issues;
  if (supersededBy.length) status.push(`- Superseded by: ${supersededBy.map(ref).join(", ")}`);
  const supersedes = linkedTo(d.links, "supersedes");
  if (supersedes.length) status.push(`- Supersedes: ${supersedes.map(ref).join(", ")}`);
  const section = (title: string, key: string) => `## ${title}\n\n${textField(d.fields, key).trim() || "_Not recorded._"}\n`;
  return [
    `# ${d.key}: ${d.summary}\n`,
    `${status.join("\n")}\n`,
    section("Context", DECISION_FIELDS.context),
    section("Options considered", DECISION_FIELDS.options),
    section("Decision", DECISION_FIELDS.outcome),
    section("Consequences", DECISION_FIELDS.consequences),
  ].join("\n");
}

/** adrFileName names the export: PLAT-12-use-postgresql-for-sessions.md. */
export function adrFileName(d: { key: string; summary: string }) {
  const slug = d.summary
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 50)
    .replace(/-+$/, "");
  return `${d.key}${slug ? `-${slug}` : ""}.md`;
}

/** riskJql is the search for a project's risks, optionally in one cell of the matrix. */
export function riskJql(projectKey: string, cell?: { likelihood: number; impact: number }) {
  const parts = [`project = ${projectKey}`, "kind = risk"];
  if (cell) parts.push(`"Risk likelihood" = ${cell.likelihood}`, `"Risk impact" = ${cell.impact}`, "statusCategory != Done");
  return parts.join(" AND ");
}

/** parseCell reads a matrix cell from the URL ("4-5": likelihood 4, impact 5). */
export function parseCell(s: string | undefined): { likelihood: number; impact: number } | undefined {
  const m = /^([1-5])-([1-5])$/.exec(s ?? "");
  return m ? { likelihood: Number(m[1]), impact: Number(m[2]) } : undefined;
}
