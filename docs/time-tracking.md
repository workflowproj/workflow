# Time tracking

Issues carry an **original estimate** and a **remaining estimate**, and people **log work**
against them. **Time spent** is the sum of an issue's worklogs. Estimates and time show in the
issue view, on board cards, in searches, in the sprint report (in hours) and on the timesheet.

## Durations

Durations are written the way people say them: `30m`, `2h`, `1h 30m`, `1.5h`, `1d`, `1w 2d 3h`.
Units are `w` (weeks), `d` (days), `h` (hours) and `m` (minutes); full words (`2 days`, `3 hours`)
work too, units may come in any order and a bare number is minutes. A day and a week are the
**working** day and week:

| Variable | Default | |
|---|---|---|
| `WFM_TIME_HOURS_PER_DAY` | `8` | hours in `1d` (more than 0, at most 24; `7.5` is fine) |
| `WFM_TIME_DAYS_PER_WEEK` | `5` | days in `1w` (more than 0, at most 7) |

So with the defaults `1w 2d` is 56 hours. Changing the settings changes how stored durations are
displayed, not the durations themselves: everything is stored in seconds. The UI reads the
settings from `GET /api/v1/time-tracking/settings`; the parser and formatter exist in Go
(`internal/timetrack`) and TypeScript (`web/src/lib/duration.ts`) with the same rules and tests.

## Estimates

Set them in the issue's **Time tracking** panel (click a value to edit it), or through the API as
`originalEstimate` and `remainingEstimate` (seconds) on `POST /issues` and `PATCH /issues/{key}`
(`null` clears one). Setting an original estimate on an issue without a remaining estimate sets
the remaining estimate to the same value. Changes are recorded in the issue history.

The panel's bar shows time logged (blue) and remaining (grey) against the original estimate.
When logged plus remaining goes past the estimate, the part beyond it turns red, the estimate is
marked on the bar and the panel says by how much the issue runs over.

## Logging work

**Log work** (in the panel or the **Work log** tab of the activity section) asks for the time
spent, when the work was done (default now, at most a day ahead), an optional description, and
what to do with the remaining estimate:

| Option | Remaining estimate |
|---|---|
| Adjust automatically (default) | goes down by the time logged, never below zero; unchanged if the issue has none |
| Set to | the value you enter |
| Leave unchanged | as it was |

Editing a worklog's time with automatic adjustment moves the remaining estimate by the
difference; deleting a worklog puts its time back (`?adjustEstimate=leave` keeps it, and
`?remainingEstimateSeconds=` sets it).

**Permissions:** whoever can edit the issue can log work on it (`issue.edit` for issues you
reported or are assigned to, `issue.edit_others` for the rest). You can change or delete your own
worklogs while you can work on the project's issues; project admins can change anyone's.
Automation rules cannot log work.

Every worklog added, changed or deleted is recorded in the history (`worklog`, with the
durations) along with any remaining estimate change, bumps the issue's version and fires an
`issue.updated` event with `fields: ["timeSpent"]` (plus `"remainingEstimate"` when it changed), so
boards and open issue views refresh live and automation rules on field changes can react.

## Where it shows

- **Board cards**: add the *Time tracking* card field (board settings, or *Configure board* on
  the board). Cards show time logged of the original estimate (`3h / 1d`, red once over), else
  the remaining estimate (`5h left`) or the time logged.
- **Search** ([search.md](search.md)): `originalEstimate`, `remainingEstimate` and `timeSpent` compare with
  durations (`remainingEstimate > 4h`, `timeSpent >= "1d 2h"`, `originalEstimate is EMPTY`) and can
  be ordered by; `worklogAuthor = currentUser()` and `worklogDate >= startOfWeek()` find issues by
  their worklogs.
- **Workflows and automation**: CEL conditions see `issue.originalEstimate`,
  `issue.remainingEstimate` and `issue.timeSpent` in seconds (`0` when unset), e.g. a validator
  `issue.timeSpent > 0` on a Done transition.
- **AI assistants**: `get_issue` shows the time tracking and `log_work` logs work
  ([mcp.md](mcp.md)).

## Sprint report in hours

The sprint report (Reports page, and the sprint burndown, burnup and health dashboard gadgets)
has an **Hours** measure next to story points and issues. Unlike issue counts and story points,
hours include sub-tasks: their estimates and the time logged on them are part of the sprint's work.

- **Committed** — the remaining estimate of the sprint's open issues when it started.
- **Remaining** each day — the remaining estimate of the issues in the sprint that are not done,
  replayed from the estimate history. Completing an issue burns what was left of its estimate.
- **Logged** — time logged on the sprint's issues while they were in the sprint, counted when it
  was logged (the moment the remaining estimate moves with it).
- **Scope** (burnup) — logged plus remaining, so it moves only when work is re-estimated, added or
  taken out, not when work is logged with the estimate adjusted.
- **Added / removed** — the remaining estimate of issues that joined or left after the start, at
  the time they did.

`GET /api/v1/sprints/{id}/report` returns these as `committedSeconds`, `completedSeconds`,
`addedSeconds` and `removedSeconds`, and per day `remainingSeconds`, `completedSeconds`,
`scopeSeconds` and `idealSeconds`.

## Timesheet

**Timesheet** (in the sidebar, or from a project's Reports page) totals the time logged per
person per day for a week or a month, in hours, on every project you can browse or on one, for
everyone or one person. Open a person's row to see the issues. Days are counted in your browser's
time zone, by when the work was done. **Export CSV** downloads the sheet: a row per person and
issue with hours per day, a total row per person and a grand total.

`GET /api/v1/worklogs?from=2026-10-19&to=2026-10-25&projects=PLAT,OPS&user=<id>&tz=Europe/London`
returns `{dates, dayTotals, total, people: [{user, days, total, issues: [{issue, days, total}]}]}`
(seconds), covering at most 366 days. Worklogs on issues you cannot browse are left out.

## API

| Endpoint | |
|---|---|
| `GET /time-tracking/settings` | `{hoursPerDay, daysPerWeek}` |
| `GET /issues/{key}/worklogs` | the issue's worklogs, in the order the work was done; each says whether you can change it (`canEdit`) |
| `POST /issues/{key}/worklogs` | `{timeSpentSeconds, startedAt?, comment?, adjustEstimate?: auto\|leave, remainingEstimateSeconds?}` |
| `PATCH /worklogs/{id}` | change time, start or comment, with the same estimate options |
| `DELETE /worklogs/{id}?adjustEstimate=&remainingEstimateSeconds=` | delete; the time goes back on the estimate unless left |
| `GET /worklogs?from&to&projects&user&tz` | timesheet |

Every issue in the API has `timeTracking: {originalSeconds?, remainingSeconds?, spentSeconds}`.
