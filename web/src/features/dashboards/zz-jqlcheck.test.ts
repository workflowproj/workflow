import { writeFileSync } from "node:fs";
import { it } from "vitest";
import { breakdownClause, scopeJql, statClause, type BreakdownBy } from "./model";
it("prints", () => {
  const out: string[] = [];
  const bys: [BreakdownBy, string][] = [["assignee", "200ec321-11e0-4433-be84-7451cf924b31"], ["assignee", ""], ["status", "x"], ["statusCategory", "in_progress"], ["priority", "high"], ["type", "story"], ["label", "blocked"], ["label", ""], ["epic", "PLAT-1"], ["epic", ""], ["sprint", "ad50af83-b312-4dca-ac05-53435cceb527"], ["sprint", ""], ["release", "ad50af83-b312-4dca-ac05-53435cceb527"], ["release", ""], ["project", "MOB"]];
  for (const [by, key] of bys) out.push(scopeJql(["PLAT", "MOB"], "sprint in openSprints() AND statusCategory != Done ORDER BY due", breakdownClause(by, key)));
  for (const s of ["open", "todo", "in_progress", "overdue", "unassigned", "created", "resolved", "median_cycle_hours"] as const) out.push(scopeJql(undefined, "assignee = currentUser() OR type = Bug", statClause(s, 30)));
  writeFileSync("/tmp/claude-0/-home-user-workflow-management/3b36552e-2368-52ca-84fd-1ab6af0b36ba/scratchpad/agent-d/jqls.json", JSON.stringify(out));
});
