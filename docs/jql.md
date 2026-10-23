# JQL

Search uses a JQL-style query language. Queries compile to parameterised SQL and are always
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
| `level` (`hierarchyLevel`) | `=` `!=` `<` `>` … | `1` epics, `0` standard, `-1` sub-tasks |
| `assignee`, `reporter` | `=` `!=` `in` `not in` `is [not] EMPTY` | email, user id, `currentUser()`, `membersOf("group")` |
| `watcher` | `=` `!=` `in` | as above |
| `priority` | `=` `!=` `in` `<` `<=` `>` `>=` | `Highest`, `High`, `Medium`, `Low`, `Lowest` (ordered) |
| `resolution` | `=` `!=` `in` `is [not] EMPTY` | `Done`, `"Won't Do"`, … or `Unresolved` |
| `labels` (`label`) | `=` `!=` `in` `not in` `is [not] EMPTY` | label |
| `sprint` | `=` `!=` `in` `not in` `is [not] EMPTY` | sprint name or id, `openSprints()`, `futureSprints()`, `closedSprints()` |
| `fixVersion` (`release`, `version`) | `=` `!=` `in` `not in` `is [not] EMPTY` | release name, `releasedVersions()`, `unreleasedVersions()` |
| `parent` (`epic`, `epicLink`) | `=` `!=` `in` `is [not] EMPTY` | issue key |
| `summary` (`title`), `description` | `~` `!~` `=` `!=` `is [not] EMPTY` | `~` is a case-insensitive substring match |
| `text` | `~` `!~` | full-text search over summary and description (web-search syntax: `"exact phrase"`, `-exclude`, `or`) |
| `comment` | `~` `!~` | text in comments |
| `created`, `updated`, `resolved`, `due`, `start` | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | dates (below) |
| `storyPoints` (`points`, `estimate`) | `=` `!=` `<` `<=` `>` `>=` `is [not] EMPTY` | number |
| custom fields | depends on type | by key or name (`severity = High`), or `cf[key]` |

Custom fields by type: `text`/`url` support `~`, `=`, `is EMPTY`; `number` and `date` support
comparisons; `select` and `multiselect` support `=`, `in`; `user` accepts the user values above;
`checkbox` accepts `true`/`false`; `version` accepts release names.

## Dates

- Absolute: `"2026-11-01"` or `"2026-11-01 14:30"`.
- Relative to now: `-7d`, `"+2w"`, `-12h`, `-30m`, `-1M`, `-1y` (units `m` minutes, `h` hours,
  `d` days, `w` weeks, `M` months, `y` years).
- Functions: `now()`, `startOfDay()`, `endOfDay()`, `startOfWeek()`, `endOfWeek()`,
  `startOfMonth()`, `endOfMonth()`, `startOfYear()`, `endOfYear()`.

## Ordering

`ORDER BY` accepts `rank`, `key`, `created`, `updated`, `resolved`, `due`, `start`, `priority`,
`summary`, `status`, `assignee`, `storyPoints` and `type`.

## Saved filters

Save any query as a filter (Issues → Save). Shared filters are visible to everyone; only the
owner (or an organisation admin) can change or delete them. Boards and scheduled automation
rules also take JQL.

## API

`GET /api/v1/search?jql=…&limit=50&offset=0` returns `{issues, total, limit, offset}`;
`GET /api/v1/search/fields` returns the fields, operators and functions for autocomplete.
A query error answers 400 with `position` (character offset) and a message.
