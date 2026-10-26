# Workflows as code

A workflow is a YAML document: statuses, the transitions between them, and the rules that
apply when an issue moves. Each issue type in a project uses one workflow. Project templates
create sensible defaults; change them in the visual editor (Project settings → Workflows), in
YAML in the same editor, or from CI with the CLI.

```yaml
kind: Workflow            # optional
name: Software development
description: To Do → In Progress → In Review → Done
statuses:
  - { name: To Do, category: todo }          # the first status is the initial one,
  - { name: In Progress, category: in_progress }  # unless another has `initial: true`
  - { name: In Review, category: in_progress }
  - { name: Done, category: done }
transitions:
  - name: Start work
    from: [To Do]
    to: In Progress
    postFunctions: [{ assignTo: actor }]
  - { name: Submit for review, from: In Progress, to: In Review }
  - name: Done
    from: [In Progress, In Review]
    to: Done
    condition: 'actor.hasRole("member") || actor.hasRole("admin")'
    validators:
      - expr: 'issue.subtasks.all(s, s.category == "done")'
        message: Finish every sub-task first.
    screen:
      - { field: resolution, required: true }
      - comment
  - { name: Reopen, from: Done, to: To Do }
  - { name: Won't do, to: Done }              # no `from`: available from any status
layout:                                       # node positions for the visual editor
  To Do: { x: 0, y: 120 }
```

## Statuses

| Key | |
|---|---|
| `name` | Up to 60 characters. Statuses are shared across workflows by name, so "In Review" is the same status everywhere (boards and searches rely on this). A name must always have the same category. |
| `category` | `todo`, `in_progress` or `done`. Categories drive boards, progress bars, reports and resolution: entering a `done` status sets the resolution (to `Done` unless a screen or post-function chose another), leaving one clears it. |
| `initial` | Optional; marks the status new issues start in (otherwise the first). |

At most 50 statuses.

## Transitions

| Key | |
|---|---|
| `name` | Shown on buttons and in history. |
| `id` | Optional; defaults to the slug of the name (`Start work` → `start-work`). API clients and smart commits use it. |
| `from` | A status name or a list. Omit it for a global transition, available from every other status. |
| `to` | Target status. |
| `condition` | CEL expression; when false the transition is hidden and refused (HTTP 403). |
| `validators` | CEL expressions (a string, or `{expr, message}`) checked after the screen is filled; failures answer 422 `validation_failed` with the messages. |
| `screen` | Fields to ask for when the transition runs: a field name or `{field, required}`. Built-in fields: `resolution`, `comment`, `assignee`, `priority`, `storyPoints`, `dueDate`, `labels`, `fixVersions`; any custom field key. A `resolution` field may list the resolutions it offers with `options` (`{field: resolution, required: true, options: [Mitigated, Accepted, Occurred]}`), instead of `Done`, `Won't Do`, `Duplicate` and `Cannot Reproduce`. A transition whose screen has required fields cannot run from a drag on the board without the dialog (422 `screen_required`). |
| `postFunctions` | Run after the move, in order. Exactly one key each: |

Post-functions:

| | Runs | |
|---|---|---|
| `assignTo: actor \| reporter \| unassigned \| <email>` | in the transaction | |
| `setField: {field, value}` | in the transaction | `priority`, `storyPoints`, `dueDate`, `labels`, `assignee`, `resolution` or a custom field key |
| `clearField: <field>` | in the transaction | |
| `notify: assignee \| reporter \| watchers` | as a background job | in-app notification, emailed per the recipients' [settings](notifications.md) |
| `webhook: <url>` | as a background job | POST of the issue as JSON (unsigned — use a [webhook subscription](automation.md#outbound-webhooks) when the receiver needs to verify the sender) |

## CEL reference

Conditions, validators and automation conditions are [CEL](https://cel.dev) expressions with a
cost limit. Variables:

| Variable | Fields |
|---|---|
| `issue` | `key`, `project`, `type` (key), `typeLevel`, `summary`, `description`, `priority`, `status`, `statusCategory`, `assignee` and `reporter` (emails, `""` when unset), `labels`, `storyPoints`, `originalEstimate`, `remainingEstimate` and `timeSpent` (seconds, `0` when unset), `resolution`, `startDate`, `dueDate` (`YYYY-MM-DD` or `""`), `fields` (custom fields by key), `subtasks` / `children` (list of `{key, summary, type, status, category}`), `parent` (`{key, type, status, category, childCount, openChildren, startDate, dueDate}` or `{}`; `openChildren` counts the parent's children not in a done status), `links` (list of `{type, direction, key, category}`), `devLinks` (list of `{kind: branch\|commit\|pr\|pipeline, state, title, merged, repository}`) |
| `actor` | `id`, `email`, `name`, `type` (`user`, `automation`, `scm`), `orgRole`, `roles` (project role keys), `permissions` |
| `transition` | `id`, `name`, `from`, `to` |
| `event` | automation triggers only: `type` and trigger data (e.g. `from`, `to`, `fields`, `number`, `branch`) |
| `now` | timestamp |

Functions: `actor.hasRole("admin")`, `actor.hasPermission("issue.delete")`, plus the standard
library and the strings, lists, sets and math extensions.

Examples:

```cel
issue.assignee != ""                                          // someone must own it
issue.storyPoints > 0                                         // estimated before starting
issue.devLinks.exists(d, d.kind == "pr" && d.merged)          // a merged PR is linked
!issue.links.exists(l, l.type == "blocks" && l.direction == "inward" && l.category != "done")
issue.type != "bug" || issue.fields.severity != ""            // bugs need a severity
actor.email == issue.reporter || actor.hasRole("admin")
```

Automation and SCM actors (`actor.type` `automation` or `scm`) pass `hasRole` and
`hasPermission` checks; smart commits run as the mapped committer, so their own roles apply.

## Versions and migration

Workflows are versioned. Publishing (editor, `PUT /api/v1/workflows/apply`, or the CLI)
creates version N+1; existing issues stay on the version they were created under until a
`workflow_migrate` job moves them. When the new version removes a status that issues still
use, publishing requires a **status mapping** (`{"Ready for QA": "In Review"}`); without it the
API answers 422 `status_mapping_required` with the statuses that need one. Validate first with
`POST /api/v1/workflows/validate`, which also reports `removedStatuses`.

Changing an issue type's workflow (Project settings → Issue types) works the same way: statuses
that exist in both workflows carry over; others need a mapping.

Boards follow a mapping: on the boards of the projects that use the workflow, the column that
shows a removed status also shows the status it maps to, unless the board already has a column
for that one. So renaming *In Review* to *Code Review* (the editor sends renames as mappings, even
when no issue is in the status) keeps that work in the same column; the old status stays in the
column until you remove it in the board's settings. Boards show work only, so changes to the
workflows of risk and decision types leave them alone.

## The CLI

```sh
export WFM_SERVER=https://wfm.example.com
export WFM_TOKEN=wfm_pat_...            # a token with the write scope (project admin)

wfm workflow validate -f workflows/dev.yaml
wfm workflow apply -f workflows/dev.yaml --project PLAT --dry-run
wfm workflow apply -f workflows/dev.yaml --project PLAT --map "Ready for QA=In Review"
wfm workflow export --id 0b6f…  > workflows/dev.yaml
```

Examples to start from are in [`examples/workflows/`](../examples/workflows): a code-review flow
that requires estimates and a linked pull request, and an incident flow.

`apply` is idempotent: it creates the workflow if the name is new in that scope, publishes a
version if the document changed, and does nothing (`unchanged`) otherwise — so it fits a GitOps
pipeline. Without `--project` it manages a shared workflow (organisation admins).
