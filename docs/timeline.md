# Timelines

A timeline plans work against the calendar. Epics and issues are bars, with the project's
sprints and releases marked above them and arrows for the dependencies between them. There are
three kinds:

| Timeline | Where | What it shows |
|---|---|---|
| A project's | The project's **Timeline** tab | That project |
| A programme's | The programme's **Timeline** tab | Its initiatives, the team epics under them and their issues ([programmes.md](programmes.md#timeline)) |
| Saved | **Timeline** in the sidebar | A group of projects you choose, such as "Payments teams" |

## Saved timelines

A saved timeline is a named group of projects planned together. The sidebar's **Timeline** always
shows one of them, or projects you have chosen; it never shows every project at once.

- **Opening the Timeline** opens the saved timeline you used last. Failing that, it opens your
  first timeline, then the first one shared with everyone. With none, it explains timelines and
  lets you choose projects.
- **To make one**, choose projects with the picker: the timeline shows them straight away,
  without saving. Then use **Save as timeline**, or **New timeline…** in the switcher.
  - Give it a name and, optionally, a description, and tick up to 50 projects.
  - Programmes aren't offered: each has its own timeline.
- **To switch timelines**, use the switcher next to the title. It lists your timelines, then the
  ones shared with everyone.
- **Edit** changes a timeline's name, description, projects and sharing.
  - **Delete** removes the timeline. Its projects and their work are untouched.
  - Only the owner can change a timeline. An organisation admin can also change or delete a
    shared one.
- The address, `/timeline?view=…`, can be bookmarked or sent to anyone who can see the timeline.

### Sharing

A timeline is private to its owner unless it's shared with everyone in the organisation.

Sharing grants no access. Each person sees only the timeline's projects they can browse, and the
header says how many are left out ("1 project you can't browse is left out"). When someone edits
a shared timeline, the projects they can't browse stay in it.

A deleted project leaves every timeline.

## Working on a timeline

- **Zoom**: **Weeks**, **Months** or **Quarters**. **Today** scrolls back to today.
- **Quick filters**: **Only overdue**, **Only conflicts**, **Assigned to me**, and **Show done**,
  which includes issues resolved in the last 90 days.
- **Counts** above the grid: in progress, overdue, dependency conflicts and unscheduled.
- **Dates**:
  - Drag a bar to move it, or drag one of its ends to change the start or due date.
  - For an issue without dates, hover its row and drag across the grid.
  - Select a bar to type its dates, clear them, or open the issue. **Enter** also opens it.
  - An epic without dates of its own spans its issues; its bar is drawn differently.
- **Dependencies**: select a bar and drag the dot at its end onto another bar. The first issue
  now blocks the second.
  - An arrow turns red when the blocking issue ends after the blocked one starts.
  - Select an arrow to see both issues or to remove the dependency.
  - An issue that blocks, or is blocked by, an issue the timeline doesn't show gets a warning.
- **Rows**: each project, then its epics, then their issues. Sub-tasks aren't shown. **Expand
  all** and **Collapse all** sit above the tree.

A timeline shows at most 2,000 issues. Beyond that it says so: choose fewer projects or narrow it
with a filter.

Changes made by other people appear as they happen.

## API

The timeline's data and the saved timelines' endpoints are in [api.md](api.md#timeline).
