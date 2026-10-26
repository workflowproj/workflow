import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { adrFileName, adrMarkdown, assessRisk, isOverdue, levelOf, linkedFrom, linkedTo, parseCell, progressLabel, riskJql, riskRating } from "./model";

const type = { id: "t", key: "decision", name: "Decision", icon: "decision", color: "#000", level: 0, kind: "decision" as const };
const ref = (key: string, summary: string, category: Schemas["StatusCategory"] = "todo") => ({ id: key, key, summary, type, status: { id: "s", name: "x", category } });
const link = (type: Schemas["LinkType"], direction: Schemas["LinkDirection"], issue: ReturnType<typeof ref>): Schemas["IssueLink"] => ({ id: issue.key, type, direction, label: "", issue });

describe("risk scoring", () => {
  it("bands scores like the server", () => {
    expect([1, 4, 5, 9, 10, 16, 20, 25].map(riskRating)).toEqual(["low", "low", "medium", "medium", "high", "high", "critical", "critical"]);
  });

  it("scores only with a likelihood and an impact from 1 to 5", () => {
    expect(assessRisk({ risk_likelihood: 4, risk_impact: 5, risk_response: "Reduce", risk_mitigation: " Plan B " })).toEqual({
      likelihood: 4,
      impact: 5,
      score: 20,
      rating: "critical",
      response: "Reduce",
      mitigation: " Plan B ",
    });
    expect(assessRisk({ risk_likelihood: 3 })).toEqual({ likelihood: 3, impact: undefined });
    expect([0, 6, 2.5, "3", null].map(levelOf)).toEqual([undefined, undefined, undefined, undefined, undefined]);
  });

  it("knows overdue reviews of open risks", () => {
    expect(isOverdue("2026-10-01", "todo", "2026-10-02")).toBe(true);
    expect(isOverdue("2026-10-02", "in_progress", "2026-10-02")).toBe(false);
    expect(isOverdue("2026-10-01", "done", "2026-10-02")).toBe(false);
    expect(isOverdue(undefined, "todo", "2026-10-02")).toBe(false);
  });

  it("builds search queries and reads matrix cells", () => {
    expect(riskJql("PLAT")).toBe("project = PLAT AND kind = risk");
    expect(riskJql("PLAT", { likelihood: 4, impact: 5 })).toBe('project = PLAT AND kind = risk AND "Risk likelihood" = 4 AND "Risk impact" = 5 AND statusCategory != Done');
    expect(parseCell("4-5")).toEqual({ likelihood: 4, impact: 5 });
    expect(parseCell("6-1")).toBeUndefined();
    expect(parseCell(undefined)).toBeUndefined();
  });
});

describe("linked work", () => {
  const links = [
    link("mitigates", "inward", ref("P-2", "Second provider", "done")),
    link("mitigates", "inward", ref("P-3", "Failover drill")),
    link("relates", "inward", ref("P-4", "Unrelated", "done")),
    link("supersedes", "outward", ref("P-5", "Old decision", "done")),
  ];
  it("counts what links in with a type, and what is done", () => {
    const m = linkedFrom(links, "mitigates");
    expect([m.total, m.done, m.issues.map((i) => i.key).join()]).toEqual([2, 1, "P-2,P-3"]);
    expect(progressLabel(m)).toBe("1 of 2 done");
    expect(progressLabel(linkedFrom(links, "implements"))).toBe("None");
    expect(linkedTo(links, "supersedes").map((i) => i.key)).toEqual(["P-5"]);
  });
});

describe("ADR export", () => {
  it("writes the record's sections in order", () => {
    const md = adrMarkdown({
      key: "PLAT-12",
      summary: "Store sessions in PostgreSQL",
      status: { name: "Superseded" },
      fields: { decision_context: "Sessions are lost on restart.", decision_options: "- Redis\n- PostgreSQL", decision_outcome: "PostgreSQL.", decision_date: "2026-10-01" },
      links: [link("supersedes", "inward", ref("PLAT-20", "Store sessions in Redis")), link("supersedes", "outward", ref("PLAT-3", "Keep sessions in memory"))],
    });
    expect(md).toBe(
      [
        "# PLAT-12: Store sessions in PostgreSQL\n",
        "- Status: Superseded\n- Date: 2026-10-01\n- Superseded by: PLAT-20 (Store sessions in Redis)\n- Supersedes: PLAT-3 (Keep sessions in memory)\n",
        "## Context\n\nSessions are lost on restart.\n",
        "## Options considered\n\n- Redis\n- PostgreSQL\n",
        "## Decision\n\nPostgreSQL.\n",
        "## Consequences\n\n_Not recorded._\n",
      ].join("\n"),
    );
  });

  it("names the file after the key and title", () => {
    expect(adrFileName({ key: "PLAT-12", summary: "Store sessions in PostgreSQL!" })).toBe("PLAT-12-store-sessions-in-postgresql.md");
    expect(adrFileName({ key: "PLAT-13", summary: "???" })).toBe("PLAT-13.md");
  });
});
