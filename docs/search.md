# Search

Search uses a JQL-like query language. Queries compile to parameterised SQL and are always
limited to projects you can browse. Errors report the character position they refer to, which
the UI highlights.

```
project = PLAT AND status in ("To Do", "In Progress") AND assignee = currentUser()
type = Bug AND priority >= High AND created >= -7d ORDER BY priority DESC, created
text ~ "checkout timeout" AND labels is EMPTY AND sprint in openSprints()
fixVersion = "2026.11" AND statusCategory != Done
```

## Syntax

- Clauses: `field operator value`, combined with `AND`, `OR`, `NOT` and parentheses.
- Lists: `field in (a, b, "c d")`, `field not in (…)`.
- Empty: `field is EMPTY`, `field is not EMPTY` (`NULL` is accepted too).
- Values are bare words or quoted strings (`"In Progress"`); keywords are case-insensitive.
- `ORDER BY field [ASC|DESC], …` — without it, search orders by `created DESC` and boards by
  rank.

## Fields

| Field (aliases) | Operators | Values |
|---|---|---|
| `project` | `=` `!=` `in` `not in` | key (old keys after a rename work) |
| `key` (`issuekey`, `issue`) | `=` `!=` `in` `not in` `<` `<=` `>` `>=` | `PLAT-12`; comparisons stay within the project |
| `status` | `=` `!=` `in` `not in` | status name |
| `statusCategory` (`category`) | `=` `!=` `in` `not in` | `"To Do"`, `"In Progress"`, `Done` |
| `type` (`issuetype`) | `=` `!=` `in` `not in` | issue type key or name |
| `kind` (`typeKind`) | `=` `!=` `in` `not in` | `work`, `risk` or `decision`: the issue type's kind ([risks-decisions.md](risks-decisions.md)) |
| `level` (`hierarchyLevel`) | `=` `!=` `<` `>` … | `1` epics, `0` standard, `-1` sub-tasks |
| `assignee`, `reporter` | `=` `!=` `in` `not in` `is [not] EMPTY` | email, user id, `currentUser()`, `membersOf("group")` |
| `watcher` | `=` `!=` `in` | as above |
| `priority` | `=` `!=` `in` `<` `<=` `>` `>=` | `Highest`, `High`, `Medium`, `Low`, `Lowest` (ordered) |
| `resolution` | `=` `!=` `in` `is [not] EMPTY` | `Done`, `"Won't Do"`, … or `Unresolved` |
| `labels` (`label`) | `=` `!=` `in` `not in` `is [not] EMPTY` | label |
| `sprint` | `=` `!=` `in` `not in` `is [not] EMPTY` | sprint name or id, `openSprints()`, `futureSprints()`, `closedSprints()` |
| `fixVersion` (`release`, `version`) | `=` `!=` `in` `not in` `is [not] EMPTY` | release name, `releasedVersions()`, `unreleasedVersions()` |
| `deployedTo` | `=` `!=` `in` `not in` `is [not] EMPTY` | environment name (any case): issues a successful deployment there contained (see [scm.md](scm.md#deployments)); `EMPTY` for issues never deployed |
| `parent` | `=` `!=` `in` `is [not] EMPTY` | issue key: the direct parent |
| `epic` (`epicLink`) | `=` `!=` `in` `is [not] EMPTY` | epic key: everything under the epic, sub-tasks included |
| `issueLinkType` | `=` `!=` `in` `not in` `is [not] EMPTY` | `blocks`, `"is blocked by"`, `relates`, `duplicates`, `"is duplicated by"`, `clones`, `"is cloned by"`, `mitigates`, `"is mitigated by"`, `implements`, `"is implemented by"`, `supersedes`, `"is superseded by"` — issues with such a link |
| `summary` (`title`), `description` | `~` `!~` `=` `!=` `is [not] EMPTY` | `~` is a case-insensitive substring match |
| `text` | `~` `!~` | full-text search over summary and description (web-search syntax: `"exact phrase"`, `-exclude`, `or`) |
| `comment` | `~` `!~` | text in comments |
| `created`, `updated`, `resolved`, `due`, `start` | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | dates (below) |
| `storyPoints` (`points`, `estimate`) | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | number |
| `originalEstimate` (`timeOriginalEstimate`), `remainingEstimate` (`timeEstimate`) | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | durations (below); `EMPTY` when never estimated |
| `timeSpent` | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | durations; the sum of the worklogs, `0` in comparisons and `EMPTY` when none were logged |
| `worklogAuthor` | `=` `!=` `in` `not in` `is [not] EMPTY` | people who logged work (as for `assignee`); `EMPTY` means no work logged |
| `worklogDate` | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | dates: issues with work done then (`!=` with none done then) |
| `"Time to first response"`, `"Time to resolution"` | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | service requests' SLA clocks: `breached()`, `running()`, `paused()`, `completed()`, or `remaining("2h")` (below) |
| `"Request type"` (`requestType`) | `=` `!=` `in` `not in` `is [not] EMPTY` | request type name or id; `EMPTY` for issues not shown in the help centre |
| `approval` | `=` `!=` `is [not] EMPTY` | `pending()`, `approved()`, `declined()`, `pendingBy(currentUser())` |
| `participant` | `=` `!=` `in` `not in` `is [not] EMPTY` | people a request is shared with (as for `assignee`) |
| custom fields | depends on type | by key or name (`severity = High`), or `cf[key]` |

Custom fields by type: `text`/`url` support `~`, `=`, `is EMPTY`; `number` and `date` support
comparisons; `select` and `multiselect` support `=`, `in`; `user` accepts the user values above;
`checkbox` accepts `true`/`false`; `version` accepts release names. Risks and decisions have
fields with reserved keys, such as `"Risk score"` (`risk_score`) and `"Risk rating"`
(`risk_rating`): `type = risk AND "Risk score" >= 10`.

## Links

Issue functions go with `issue in` (or `key in`):

- `issue in linkedIssues("PLAT-1")` — issues linked to PLAT-1 either way; with a link type,
  `linkedIssues("PLAT-1", "is blocked by")` is what blocks PLAT-1 and
  `linkedIssues("PLAT-1", "blocks")` what PLAT-1 blocks.
- `issue in blockedIssues()` — issues blocked by at least one issue that is not done.
- `issue in blockingIssues()` — issues blocking at least one issue that is not done.

```
sprint in openSprints() AND issue in blockedIssues()          blocked sprint work
epic = PLAT-1 AND statusCategory != Done                       what's left in an epic
issue in linkedIssues("PLAT-42", "is blocked by") AND statusCategory != Done
issue in linkedIssues("PLAT-40", "is mitigated by")           the work mitigating a risk
kind = decision AND issueLinkType = "is superseded by"         decisions that were replaced
```

## Service desk

Service requests have SLA clocks (see [service-desk.md](service-desk.md#slas)). Compare a clock
with a state — `breached()` (past its goal, running or stopped), `running()`, `paused()` or
`completed()` (stopped, met or missed) — or its time left with `remaining()`: working time in
the goal's calendar, so `< remaining("2h")` matches clocks with less than two working hours
left (a paused clock by what it has left), and `> remaining("0m")` leaves out clocks already
past their goal. Units are `m`, `h`, `d` (24 hours) and `w`. `IS EMPTY` matches requests
without that SLA.

```
"Time to first response" = breached() AND assignee is EMPTY
"Time to resolution" < remaining("2h") AND "Time to resolution" > remaining("0m")
"Request type" = "Get IT help" AND approval = pendingBy(currentUser())
participant = currentUser() OR reporter = currentUser()
resolution = Unresolved ORDER BY "Time to resolution" ASC
```

## Durations

Time tracking fields take durations as in the issue view: `30m`, `2h`, `1d`, `1w`, or several
units in quotes (`"1d 4h"`). A day and a week are the working day and week
(`WFM_TIME_HOURS_PER_DAY`, `WFM_TIME_DAYS_PER_WEEK`; 8 hours and 5 days by default), and a bare
number is minutes. See [time-tracking.md](time-tracking.md).

```
remainingEstimate > 4h AND sprint in openSprints()
timeSpent >= "1d 2h" ORDER BY timeSpent DESC
worklogAuthor = currentUser() AND worklogDate >= startOfWeek()
originalEstimate is EMPTY AND statusCategory != Done
```

## Dates

- Absolute: `"2026-11-01"` or `"2026-11-01 14:30"`.
- Relative to now: `-7d`, `"+2w"`, `-12h`, `-30m`, `-1M`, `-1y` (units `m` minutes, `h` hours,
  `d` days, `w` weeks, `M` months, `y` years).
- Functions: `now()`, `startOfDay()`, `endOfDay()`, `startOfWeek()`, `endOfWeek()`,
  `startOfMonth()`, `endOfMonth()`, `startOfYear()`, `endOfYear()`.

## Ordering

`ORDER BY` accepts `rank`, `key`, `created`, `updated`, `resolved`, `due`, `start`, `priority`,
`summary`, `status`, `assignee`, `storyPoints`, `type`, `originalEstimate`, `remainingEstimate`,
`timeSpent`, `"Time to first response"` and `"Time to resolution"` (soonest breach first; stopped
clocks and issues without the SLA last).

## Saved filters

Save any query as a filter (Issues → Save). Shared filters are visible to everyone; only the
owner (or an organisation admin) can change or delete them. Boards and scheduled automation
rules use the same query language.

## API

`GET /api/v1/search?jql=…&limit=50&offset=0` returns `{issues, total, limit, offset}`;
`GET /api/v1/search/fields` returns the fields, operators and functions for autocomplete.
A query error answers 400 with `position` (character offset) and a message.
