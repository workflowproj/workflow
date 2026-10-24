import type { DashboardInput, Gadget, GadgetConfig, GadgetType } from "./model";

/** Which projects a template asks for before it can be built. */
export type TemplateProjects = "none" | "one" | "many";

export type DashboardTemplate = {
  id: "blank" | "sprint" | "flow" | "mine";
  name: string;
  description: string;
  projects: TemplateProjects;
  /** What the gadgets show, for the picker. */
  includes: string[];
  build: (projectKeys: string[]) => DashboardInput;
};

const g = (id: string, type: GadgetType, width: 1 | 2 | 3, config: GadgetConfig, title?: string): Gadget => ({ id, type, width, config, ...(title ? { title } : {}) });

const OPEN = "statusCategory != Done";

/*
 * Rows are laid out for the three-column grid: each row's widths add up to three, so the
 * dashboard reads as tidy rows before anyone rearranges it.
 */
export const TEMPLATES: DashboardTemplate[] = [
  {
    id: "sprint",
    name: "Sprint dashboard",
    description: "How the current sprint is going: burndown, burnup, velocity, who has what and what's at risk.",
    projects: "one",
    includes: ["Sprint health", "Burndown", "Burnup", "Velocity", "Open work by assignee", "Overdue or blocked work"],
    build: ([key]) => {
      const sprint = `sprint in openSprints() AND ${OPEN}`;
      return {
        name: `Sprint dashboard · ${key}`,
        description: `The active sprint in ${key}.`,
        shared: true,
        gadgets: [
          g("health", "sprint_health", 1, { projectKey: key }),
          g("burndown", "sprint_burndown", 2, { projectKey: key }),
          g("burnup", "sprint_burnup", 2, { projectKey: key }),
          g("velocity", "velocity", 1, { projectKey: key, count: 6 }),
          g("assignees", "breakdown", 1, { projectKeys: [key], jql: sprint, by: "assignee", measure: "issues", chart: "bar" }, "Open sprint work by assignee"),
          g("at-risk", "issue_list", 2, { projectKeys: [key], jql: `${sprint} AND (due < startOfDay() OR labels = blocked) ORDER BY priority DESC, due ASC`, limit: 10 }, "Overdue or blocked"),
        ],
      };
    },
  },
  {
    id: "flow",
    name: "Team flow",
    description: "How work moves: what's open, what came in and got done, where it piles up and how long it takes.",
    projects: "many",
    includes: ["Open, created and resolved", "Cumulative flow", "Created vs resolved", "Issues by status", "Median cycle time", "Cycle time"],
    build: (keys) => {
      const projectKeys = keys.length ? keys : undefined;
      const scope = keys.length ? keys.join(", ") : "all projects";
      return {
        name: keys.length === 1 ? `Team flow · ${keys[0]}` : "Team flow",
        description: `Flow of work in ${scope} over the last 30 days.`,
        shared: true,
        gadgets: [
          g("open", "stat", 1, { projectKeys, stat: "open" }),
          g("created", "stat", 1, { projectKeys, stat: "created", days: 30 }),
          g("resolved", "stat", 1, { projectKeys, stat: "resolved", days: 30 }),
          g("flow", "cumulative_flow", 3, { projectKeys, days: 30, flowBy: "category" }),
          g("created-resolved", "created_resolved", 2, { projectKeys, days: 30, interval: "day" }),
          g("by-status", "breakdown", 1, { projectKeys, jql: OPEN, by: "status", measure: "issues", chart: "bar" }, "Open issues by status"),
          g("median-cycle", "stat", 1, { projectKeys, stat: "median_cycle_hours", days: 30 }),
          g("cycle-time", "cycle_time", 2, { projectKeys, days: 30 }),
        ],
      };
    },
  },
  {
    id: "mine",
    name: "My work",
    description: "Everything assigned to you across projects: what's open, what's late and what you finished.",
    projects: "none",
    includes: ["Open, in progress and overdue", "My open issues", "By priority", "Resolved", "Cycle time"],
    build: () => {
      const mine = "assignee = currentUser()";
      return {
        name: "My work",
        description: "Issues assigned to me.",
        shared: false,
        gadgets: [
          g("open", "stat", 1, { jql: mine, stat: "open" }, "Assigned to me"),
          g("in-progress", "stat", 1, { jql: mine, stat: "in_progress" }),
          g("overdue", "stat", 1, { jql: mine, stat: "overdue" }),
          g("list", "issue_list", 2, { jql: `${mine} AND ${OPEN} ORDER BY priority DESC, due ASC`, limit: 10 }, "My open issues"),
          g("priority", "breakdown", 1, { jql: `${mine} AND ${OPEN}`, by: "priority", measure: "issues", chart: "bar" }, "My open issues by priority"),
          g("resolved", "stat", 1, { jql: mine, stat: "resolved", days: 30 }, "Resolved by me"),
          g("cycle-time", "cycle_time", 2, { jql: mine, days: 30 }, "My cycle time"),
        ],
      };
    },
  },
  {
    id: "blank",
    name: "Blank dashboard",
    description: "Start empty and add the gadgets you want.",
    projects: "none",
    includes: [],
    build: () => ({ name: "New dashboard", description: "", shared: false, gadgets: [] }),
  },
];

export function templateById(id: DashboardTemplate["id"]): DashboardTemplate {
  return TEMPLATES.find((t) => t.id === id) ?? TEMPLATES[TEMPLATES.length - 1];
}
