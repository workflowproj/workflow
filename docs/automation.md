# Automation and webhooks

## Rules

Project settings → Automation. A rule has a **trigger**, an optional **condition** (CEL) and
one or more **actions**, run in order as the automation user. Every run is recorded with its
outcome (success, skipped, failed) and message.

### Triggers

| Trigger | Fires when | Options |
|---|---|---|
| `issue.created` | an issue is created | |
| `issue.transitioned` | an issue changes status | `toStatus`: only when entering this status |
| `issue.field_changed` | an issue is edited | `field`: only when this field changed (`priority`, `assignee`, `labels`, a custom field key, …) |
| `pr.opened`, `pr.merged` | a linked pull/merge request opens (or leaves draft) / merges | |
| `commit.pushed`, `branch.created` | a commit or branch mentioning the issue is first seen | |
| `pipeline.completed` | a linked pipeline/workflow run succeeds or fails | |
| `schedule` | on a cron schedule (UTC, 5 fields) | `cron`, `jql`: the issues to act on (up to 100 per run) |

### Conditions

CEL, with the same variables as workflow conditions plus `event` — the trigger's data, e.g.
`event.to` (status entered), `event.fields` (changed fields), `event.number`/`event.title`/`event.url`
(pull requests), `event.status` (pipelines). See [workflows.md](workflows.md#cel-reference).

```cel
issue.type == "bug" && issue.priority in ["highest", "high"]
event.status == "failed" && issue.statusCategory != "done"
```

### Actions

| Action | Options |
|---|---|
| `transition` | `status`: target status name; runs the transition that leads there (conditions and validators apply; screens with required fields are skipped) |
| `assign` | `assignee`: `actor`, `reporter`, `unassigned` or an email |
| `set_field` | `field`, `value` (system fields like `priority`, `labels`, `storyPoints`, `dueDate`, or a custom field) |
| `comment` | `body` (Markdown); `{{issue.key}}`, `{{issue.summary}}` and `{{event.<name>}}` are replaced |
| `add_label` | `label` |
| `notify` | `recipients`: `assignee`, `reporter` or `watchers` |
| `webhook` | `url`: POSTs `{event: "automation", issue, trigger, data, sentAt}` (unsigned) |

Examples:

- *Move to In Review when a PR opens*: trigger `pr.opened`, action transition → `In Review`.
- *Close when merged*: trigger `pr.merged`, actions transition → `Done`, comment `Merged in #{{event.number}}`.
- *Triage new bugs*: trigger `issue.created`, condition `issue.type == "bug"`, actions set
  priority `high`, add label `needs-triage`, notify watchers.
- *Weekly stale check*: trigger `schedule` `0 8 * * 1` with JQL
  `statusCategory = "In Progress" AND updated <= -14d`, action add label `stale`.

### Loops

A rule never triggers itself. Rules can trigger each other (a transition made by one rule is an
event for the others), up to `WFM_AUTOMATION_MAX_DEPTH` (5) levels; deeper runs are recorded as
skipped.

## Outbound webhooks

Project settings → Webhooks (or Administration → Webhooks for all projects) sends events to
other systems: `issue.created`, `issue.updated`, `issue.transitioned`, `issue.deleted`,
`comment.created`, `sprint.started`, `sprint.completed`, `release.released`, or `*`.

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

**Network safety**: outbound requests (webhooks, automation and post-function webhooks) refuse
private, loopback, link-local and other internal addresses, including after DNS resolution and
redirects. Set `WFM_WEBHOOK_ALLOW_PRIVATE=true` only if you need to call internal services.
