# Programmes

A **programme** plans work across several teams' projects. It holds **initiatives**: outcomes the
teams work toward together, such as *Self-serve checkout*. Each team keeps planning in its own
project, with its own board, backlog, sprints and releases. The one link across projects is an
epic's parent: an epic in a team project can belong to one of the programme's initiatives.

The programme then shows:

- each initiative's epics, team by team;
- how much of the work under them is done;
- when it is likely to finish;
- what is late or blocked by another team.

```
Programme CHK                Team PLAT                 Team WEB
  CHK-1 Self-serve checkout ── PLAT-14 Order API  ──┐
                            └─ WEB-3 Checkout redesign ── WEB-21 Card form …
                                       stories, sprints and boards stay in each team
```

## Setting one up

1. Create a project from the **Programme** template (Projects → Create project). It comes with:
   - an **Initiative** issue type (level 2, above epics), with a Proposed → In Progress → Done
     workflow;
   - an **Action** type for the programme's own tasks, with an Actions board;
   - risks and decisions, for programme-level risks and decisions (see
     [risks-decisions.md](risks-decisions.md)).
2. In the programme's **Settings → Teams**, add the team projects. Only the programme's admins can
   add teams, and only projects they can browse. Service projects and other programmes can't be
   teams.
3. Create initiatives on the **Roadmap** tab (**New initiative**).
4. Give each epic its initiative:
   - on the epic, with the **Initiative** field in its details;
   - when creating an epic, with the Initiative picker;
   - from the roadmap's list of team epics under no initiative.

A programme's tabs are **Roadmap**, **Timeline**, **Dependencies**, **Board** (its actions),
**Issues**, **Risks**, **Decisions** and **Reports**. A team project lists the programmes it
belongs to on the API (`GET /projects/{key}/programmes`), and its epics' Initiative field offers
their initiatives.

## The hierarchy

| Level | Type | Where |
|---|---|---|
| 2 | Initiative | The programme |
| 1 | Epic | A team project, under an initiative or not |
| 0 | Story, task, bug | The epic's project |
| -1 | Sub-task | Its parent's project |

- **Crossing projects:** only an epic's parent may be in another project. That parent must be an
  initiative of a programme the epic's project is a team of. Everything below an epic stays in
  the epic's project, so boards, sprints, the backlog and ranking work as before.
- **Moving an epic:** an epic can move from one initiative to another, in the same or another
  programme. Its history records each change with the initiatives' keys.
- **Removing a team:** taking a team out of a programme unlinks its epics from the programme's
  initiatives. The settings page asks first, and each epic's history records it.
- **Deleting:** deleting an initiative, or the whole programme, leaves the team epics in place,
  without an initiative.

## Who sees what

Being one of a programme's teams gives nobody access to anything. Everything a programme shows
is limited to what the viewer can already browse.

| Someone who can… | Sees |
|---|---|
| Browse the programme but not a team | The team counted ("1 team you can't browse is left out"), never named. None of its epics or work, and no progress from it |
| Browse a team but not the programme | The team's epics without their initiative: the epic says "An initiative you can't see". They can't change or remove it (403) and can't pick that programme's initiatives |
| Browse both | Everything |

A dependency on an issue the viewer can't browse is counted, not named: "Blocked by 1 unfinished
issue in a project you can't see".

## Roadmap

The **Roadmap** tab lists the programme's open initiatives in rank order (**Show done** adds
those finished in the last 90 days). Each initiative shows:

- **Progress:** the issues under its epics, in the teams the viewer can browse, by status, with
  story points.
- **Due date and forecast.** An epic's forecast is the latest due date of the epic and the issues
  under it. An initiative's forecast is the latest of its epics'. The forecast shows in red when
  it is after the due date.
- **Its epics, team by team,** each with its own progress, forecast and flags.

The flags:

| Flag | On an epic | On an initiative |
|---|---|---|
| Late | Its forecast is after the initiative's due date | Some of its epics are late |
| Overdue | Its due date has passed and it isn't done | The same |
| Unscheduled | No dates, and no due dates under it | No dates, and no forecast from its epics |
| Blocked | It or an issue under it is blocked by unfinished work in another project | Some of its epics are blocked |

Below the initiatives, **Team epics under no initiative** lists the teams' open epics that don't
yet say which initiative they serve. People who can edit an epic can link it to an initiative
from there.

## Timeline

The programme's **Timeline** nests each initiative, the team epics under it and their issues. It
then lists, team by team, the epics under no initiative and issues with no epic. Dates roll up:

- an epic without dates spans its issues;
- an initiative without dates spans its epics.

Dependency arrows work across teams. An issue that blocks, or is blocked by, an issue the
timeline doesn't show gets a warning naming it, or saying it is in a project the viewer can't
browse. Everything else works as on a project's timeline; the API is in [api.md](api.md#timeline).

## Dependencies

The **Dependencies** tab shows who waits on whom:

- a matrix of unfinished `blocks` links between the programme's projects, with blocking teams in
  the rows and blocked teams in the columns;
- each link, listing first the ones whose dates clash (the blocker ends after the blocked issue
  starts);
- what the programme's issues wait on, or hold up, outside it.

## Reports

The programme's **Reports** cover the programme and its teams (those the viewer can browse):

- open work by team;
- cumulative flow;
- created vs resolved;
- cycle time.

Velocity and sprint reports stay with each team, whose sprints differ. Any dashboard gadget can
cover the same projects with `project in programmeProjects("CHK")`.

## Search

| Query | Matches |
|---|---|
| `initiative = CHK-1` | Everything under the initiative: its epics, their issues and sub-tasks, in every team |
| `initiative is EMPTY` | Issues with no initiative above them |
| `issue in descendantsOf("CHK-1")` | Everything below any issue (three levels down at most) |
| `project in programmeProjects("CHK")` | The programme's own issues and its teams' |

`epic = KEY` means epics only: an initiative is not an epic, and an epic under an initiative has
no epic of its own. Like every issue key in a query, the key in these only matches issues the
viewer can browse.

## Automation

A rule acts in its own project. An action whose target is the parent or the children skips those
in another project, and the rule's run log says so: "skipped 1 parent in another project".

- A team's rule that rolls dates up to an epic's parent leaves the initiative alone.
- A programme's rule doesn't change team epics.
- **Roll up dates** counts only children in the rule's project.

The roadmap's forecast shows when an initiative is likely to finish without changing its dates.

## API

| | |
|---|---|
| `GET /projects/{key}/programme?doneDays=90` | The roadmap: initiatives, their epics, progress, forecasts and flags, and team epics under no initiative |
| `GET /projects/{key}/programme/teams` | The teams the caller can browse and how many others there are |
| `POST /projects/{key}/programme/teams` `{"projectKey": "WEB"}` | Add a team (programme admins) |
| `DELETE /projects/{key}/programme/teams/{teamKey}` | Remove a team; returns how many epics were unlinked |
| `GET /projects/{key}/programmes` | The programmes a project is a team of |
| `GET /timeline?programme=CHK` | The programme's timeline. `externalLinks` lists dependencies on issues it doesn't show |
| `PATCH /issues/{epicKey}` `{"parentKey": "CHK-1"}` | Put an epic under an initiative (`null` takes it out) |

The MCP server's `get_programme` tool reads the roadmap; see [mcp.md](mcp.md).
