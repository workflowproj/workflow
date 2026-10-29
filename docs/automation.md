# Automation and webhooks

## Rules

Project settings → Automation. A rule has a **trigger**, an optional **condition** (CEL) and
one or more **actions**, run in order as the automation user. Every run is recorded with its
outcome (success, skipped, failed) and message.

### Triggers

| Trigger | Fires when | Options |
|---|---|---|
| `issue.created` | an issue is created | |
| `issue.transitioned` | an issue changes status | `toStatus`: only when entering this status; or `toCategory`: entering any `todo`, `in_progress` or `done` status |
| `issue.field_changed` | an issue is edited | `field`: only when one of these fields changed, comma-separated (`startDate, dueDate`, `priority`, `assignee`, `labels`, `parent`, a custom field key, …) |
| `pr.opened`, `pr.merged` | a linked pull/merge request opens (or leaves draft) / merges | |
| `commit.pushed`, `branch.created` | a commit or branch mentioning the issue is first seen | |
| `pipeline.completed` | a linked pipeline/workflow run succeeds or fails | |
| `deployment.completed` | a deployment containing the issue succeeds or fails (see [scm.md](scm.md#deployments)); once per outcome | `environment`: only this environment (any case); `status`: `success` or `failed` |
| `schedule` | on a cron schedule (UTC, 5 fields) | `cron`, `jql`: the issues to act on (up to 100 per run) |
| `sla.breached` | a service request's SLA clock passes its goal (once per clock; see [service-desk.md](service-desk.md#slas)) | `event.sla` is the SLA's name, `event.metric` `first_response` or `resolution` |

### Conditions

CEL, with the same variables as workflow conditions plus `event` — the trigger's data, e.g.
`event.to` (status entered), `event.fields` (changed fields), `event.number`/`event.title`/`event.url`
(pull requests), `event.status` (pipelines), `event.environment`/`event.status`/`event.url`/`event.ref`/
`event.sha`/`event.deployer`/`event.repository` (deployments). See [workflows.md](workflows.md#cel-reference).

```cel
issue.type == "bug" && issue.priority in ["highest", "high"]
event.status == "failed" && issue.statusCategory != "done"          // pipeline.completed
has(issue.parent.key) && issue.parent.category != "done"            // guard before using parent fields
```

`issue.parent` is `{}` for issues without a parent, so check `has(issue.parent.key)` before
reading its fields. `issue.parent.openChildren` counts the parent's children that are not in a
done status — including this issue while it is still open; when a sub-task enters a done status,
the count already excludes it. When several children close at the same moment, each run may see
`openChildren == 0`; adding `issue.parent.category != "done"` keeps the parent action to one run.

### Actions

Every action takes a **target**: `issue` (the issue that triggered the rule, the default),
`parent` (a story's epic, a sub-task's story) or `children` (each child). This is how rules keep
related work in step — start an epic when its first story starts, finish a story when its last
sub-task is done, stretch an epic's dates over its stories.

| Action | Options |
|---|---|
| `transition` | `status`: target status name, or `category`: `todo` / `in_progress` / `done` (the first status of that category the workflow allows; nothing happens when the issue is already in it). Conditions and validators apply; screens with required fields are skipped |
| `assign` | `assignee`: `actor`, `reporter`, `unassigned` or an email |
| `set_field` | `field`, `value` (system fields like `priority`, `labels`, `storyPoints`, `dueDate`, or a custom field) |
| `comment` | `body` (Markdown); `{{issue.key}}` / `{{issue.summary}}` (the issue the action runs on), `{{trigger.key}}` / `{{trigger.summary}}` (the issue that fired the rule) and `{{event.<name>}}` are replaced |
| `add_label` | `label` |
| `notify` | `recipients`: `assignee`, `reporter` or `watchers` (in-app, and emailed per their [settings](notifications.md)) |
| `webhook` | `url`: POSTs `{event: "automation", issue, trigger, data, sentAt}`; optional `secret` (signs it, see [below](#automation-webhooks)), `authHeader` and `authValue` |
| `chat` | `channelId` (one of the project's [Slack/Mattermost channels](chat.md)), `body`: message with the same templates as comments |
| `rollup_dates` | sets the target's start and due dates to span its children's dates — typically with target `parent`, so epics on the timeline cover their stories |

Project settings → Automation offers these as **templates** to start from.

Examples:

- *Start the epic with its first story*: trigger `issue.transitioned` with `toCategory`
  `in_progress`, condition `issue.typeLevel == 0 && issue.parent.category == "todo"`, action
  transition target `parent` category `in_progress`.
- *Finish the story with its last sub-task*: trigger `issue.transitioned` `toCategory` `done`,
  condition `issue.typeLevel == -1 && issue.parent.openChildren == 0`, actions transition target
  `parent` category `done`, comment target `parent` "All sub-tasks done ({{trigger.key}} was last)".
- *Keep epic dates covering their stories*: trigger `issue.field_changed` with field
  `startDate, dueDate, parent`, action `rollup_dates` target `parent`.
- *Move to In Review when a PR opens*: trigger `pr.opened`, action transition → `In Review`.
- *Close when merged*: trigger `pr.merged`, actions transition → `Done`, comment `Merged in #{{event.number}}`.
- *Move issues to Done when they reach production* (a template, created turned off so you can
  check the environment's name first): trigger `deployment.completed` with environment
  `production` and status `success`, actions transition category `done`, comment
  `Deployed to {{event.environment}}: {{event.url}}`.
- *Triage new bugs*: trigger `issue.created`, condition `issue.type == "bug"`, actions set
  priority `high`, add label `needs-triage`, notify watchers.
- *Weekly stale check*: trigger `schedule` `0 8 * * 1` with the query
  `statusCategory = "In Progress" AND updated <= -14d`, action add label `stale`.
- *Remind risk owners about reviews due this week* (offered when the project keeps
  [risks and decisions](risks-decisions.md)): trigger `schedule` `0 8 * * 1` with the query
  `kind = risk AND statusCategory != Done AND due <= 7d AND assignee is not EMPTY`, action notify
  `assignee`.

### Loops

A rule never triggers itself. Rules can trigger each other (a transition made by one rule is an
event for the others), up to `WFM_AUTOMATION_MAX_DEPTH` (5) levels; deeper runs are recorded as
skipped.

## Outbound webhooks

Project settings → Webhooks (or Administration → Webhooks for all projects) sends events to
other systems: `issue.created`, `issue.updated`, `issue.transitioned`, `issue.deleted`,
`comment.created`, `sprint.started`, `sprint.completed`, `release.released`, `sla.breached`,
`deployment.completed`, or `*`. `deployment.completed` is delivered once for each issue the
deployment contains, with the deployment in `data` (`environment`, `status`, `url`, `ref`, `sha`,
`deployer`, `repository`, `provider`, `deploymentId`).
In service projects, `comment.created` carries `data.visibility` (`public` for replies to the
customer, `internal` for notes), and comments added by automation rules are internal notes.

Each delivery is a `POST` with a JSON body:

```json
{
  "event": "issue.transitioned",
  "timestamp": "2026-11-02T10:15:00Z",
  "project": { "id": "…", "key": "PLAT", "name": "Platform" },
  "actor": { "id": "…", "displayName": "Ada Lovelace", "email": "ada@example.com" },
  "issue": { "key": "PLAT-12", "summary": "…", "status": { "name": "Done", "category": "done" }, "…": "…" },
  "data": { "from": "In Review", "to": "Done", "transition": "Done" }
}
```

Headers: `X-WFM-Event`, `X-WFM-Delivery` (unique id — use it to de-duplicate), and
`X-WFM-Signature-256: sha256=<hex HMAC-SHA256 of the raw body with the webhook's secret>`.
The secret is shown once when the webhook is created. Verify it like this:

```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
ok := hmac.Equal([]byte(r.Header.Get("X-WFM-Signature-256")), []byte("sha256="+hex.EncodeToString(mac.Sum(nil))))
```

Deliveries are queued after the change commits and retried with exponential backoff on
network errors and non-2xx answers; the last deliveries and their status are listed per
webhook, with *Send test*.

### Automation webhooks

A rule's `webhook` action can carry two secrets for the receiver:

- `secret` signs the body exactly like an outbound webhook: `X-WFM-Signature-256:
  sha256=<hex HMAC-SHA256>`, verified with the code above.
- `authValue` is sent in the `authHeader` header (default `Authorization`), for receivers
  that want a token, e.g. `Bearer <token>` or an API key in `X-Api-Key`. `Host`,
  `Content-Type`, `Content-Length`, `User-Agent` and `X-WFM-*` can't be used.

Both are write-only: they are stored encrypted with `WFM_SECRET_KEY`, and the API only
reports `secretSet` and `authSet`. When saving a rule, leave a secret out to keep it, or send
`""` to remove it. Changing a webhook's `url` drops its stored secrets, so editing a rule
can't send them to a different server; enter them again with the new URL.

**Network safety**: outbound requests (webhooks, automation and post-function webhooks) refuse
private, loopback, link-local and other internal addresses, including after DNS resolution and
redirects. Set `WFM_WEBHOOK_ALLOW_PRIVATE=true` only if you need to call internal services.
