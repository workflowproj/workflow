# Metrics and dashboards

## Issue metrics

Every issue's history (status, sprint and story point changes) gives its timing, shown in the
issue view and returned by `GET /api/v1/issues/{key}/metrics`:

| Metric | Measures |
|---|---|
| Lead time | created → resolved |
| Cycle time | first entering an in-progress status → resolved (or → now, "in progress for", while open) |
| Age | created → now (or → resolution) |
| In status | since entering the current status (also "days in column" on board cards) |
| Time in status | total time in each status visited, with the number of visits |
| Reopened | times it left a done status |
| Sprints | sprints it has been part of (carried over work shows here) |

## Sprint reports

The burndown replays the sprint day by day: the issues committed when it started, then each
issue added or taken out, each re-estimate and each completion. So the line steps up when scope
is added, and the report lists committed, completed, added and removed work. The burnup draws
the same data as two lines, scope and completed. Projects that don't estimate can use issue counts
instead of story points, and teams that track time can switch to hours: the remaining estimate
burning down and the time logged ([time-tracking.md](time-tracking.md)). Velocity compares
committed and completed points over the last completed sprints.

The **Timesheet** (sidebar) totals the time people logged per day, for a week or a month, by
project and person, and exports CSV.

## Flow metrics

Over any set of projects, narrowed by a query:

- **Summary** — open (to do / in progress), created and resolved in the window, overdue,
  unassigned, open and resolved story points, median cycle time.
- **Cumulative flow** — issues in each status category (or status) at the end of each day.
  Issues resolved before the window are left out, so the done band is the work finished in it.
- **Created vs resolved** — per day or week (weeks start on Monday), with how many were open at
  the end of each.
- **Cycle time** — each issue resolved in the window, with the median, average and 85th
  percentile.
- **Breakdown** — issues and points grouped by status, status category, priority, type,
  assignee, label, epic, sprint, release or project.

## Dashboards

**Dashboards** (in the sidebar) are pages of gadgets: sprint burndown, burnup and health,
velocity, cumulative flow, created vs resolved, cycle time, breakdowns, single numbers and issue
lists. Start from a template (sprint, team flow, my work) or from scratch, arrange gadgets on a
three-column grid, and share a dashboard with the whole organisation. A shared dashboard stores
only its settings: every gadget loads its data with the viewer's own access, so nobody sees
issues from projects they can't browse. Only the owner (or an organisation admin) can change a
dashboard; others can make a copy.

## API

| Endpoint | |
|---|---|
| `GET /issues/{key}/metrics` | issue timing |
| `GET /sprints/{id}/report` | burndown/burnup and scope change |
| `GET /projects/{key}/velocity?count=7` | committed and completed per sprint |
| `GET /metrics/summary`, `/metrics/flow`, `/metrics/created-resolved`, `/metrics/breakdown?by=…`, `/metrics/cycle-time` | flow metrics; all take `projects=A,B`, `jql=` and (except breakdown) `days=` |
| `GET/POST /dashboards`, `GET/PUT/DELETE /dashboards/{id}` | dashboards |
| `GET /worklogs?from=&to=&projects=&user=&tz=` | timesheet: time logged per person, issue and day |

The same metrics are available to AI assistants through the `get_metrics` and
`get_sprint_report` tools ([mcp.md](mcp.md)).
