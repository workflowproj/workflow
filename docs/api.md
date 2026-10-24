# REST API

Everything the UI does goes through the REST API at `/api/v1`, described by
[`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.1). Generate a client for your language
from it, or use `curl`.

## Authentication

Use a personal access token or a service-account token as a bearer token:

```sh
curl -H "Authorization: Bearer $WFM_TOKEN" https://wfm.example.com/api/v1/search?jql=assignee%3DcurrentUser%28%29
```

Scopes: `read` (GET only), `write` (everything except `/admin`), `admin`. The token never has
more access than its owner. Browser sessions use a cookie and must send `X-Requested-With` on
requests that change data.

## Conventions

- Issues are addressed by key (`PLAT-12`; old keys after a project rename still work) or id.
- `PATCH /issues/{key}` changes only the fields you send; send `null` to clear a field. Include
  `version` (from the last read) to avoid overwriting someone else's change — a stale version
  answers 409 `version_conflict`.
- Lists accept `limit` (and `offset` or a `before` cursor where noted).
- Dates are `YYYY-MM-DD`; timestamps are RFC 3339 UTC.

## Errors

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem documents:

```json
{
  "status": 422,
  "title": "Unprocessable Entity",
  "code": "validation_failed",
  "detail": "Finish every sub-task first.",
  "errors": [{ "field": "validators[0]", "message": "Finish every sub-task first." }]
}
```

Codes worth handling:

| Code | Status | Meaning |
|---|---|---|
| `invalid` | 400 | a field is invalid; see `errors` |
| `version_conflict` | 409 | the issue changed since you read it |
| `transition_not_available` | 422 | the transition does not start from the issue's current status |
| `transition_not_allowed` | 422 | a board move has no transition to the target status |
| `forbidden` | 403 | missing permission, or the transition's condition is false |
| `screen_invalid` / `screen_required` | 422 | the transition needs screen fields (`/issues/{key}/transitions` lists them) |
| `validation_failed` | 422 | a workflow validator refused the transition |
| `status_mapping_required` | 422 | publishing a workflow or changing a type's workflow removes statuses in use; resend with `statusMapping` |
| `duplicate_key` | 409 | project key in use |
| `too_large` | 413 | attachment over the size limit |

A JQL error answers 400 with `position`, the character offset of the problem.

## Common calls

```sh
# Create an issue
curl -X POST $WFM/api/v1/issues -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d '{"projectKey":"PLAT","typeKey":"bug","summary":"Checkout fails on Safari","priority":"high"}'

# What can I do with it, then do it
curl $WFM/api/v1/issues/PLAT-42/transitions -H "Authorization: Bearer $T"
curl -X POST $WFM/api/v1/issues/PLAT-42/transitions -H "Authorization: Bearer $T" \
  -H "Content-Type: application/json" -d '{"transitionId":"start-work"}'

# Search
curl -G $WFM/api/v1/search -H "Authorization: Bearer $T" \
  --data-urlencode 'jql=project = PLAT AND sprint in openSprints() ORDER BY rank'
```

## Timeline

`GET /api/v1/timeline?projects=PLAT,OPS&jql=…&doneDays=90` returns what the timeline draws:
epics and standard issues (not sub-tasks) with their dates and parents, the `blocks` dependencies
between them (`sourceKey` blocks `targetKey`), and each project's sprints and releases. Omit
`projects` for every project you can browse. Change dates with `PATCH /issues/{key}`
(`startDate`, `dueDate`) and add dependencies with `POST /issues/{key}/links` (`type: blocks`).

## Slack and Mattermost

Project admins manage chat channels with `GET/POST /projects/{key}/chat-channels`,
`PATCH/DELETE /chat-channels/{id}`, `POST /chat-channels/{id}/test` and
`GET /chat-channels/{id}/deliveries`. The webhook URL is write-only. See [chat.md](chat.md).

## Live events

`GET /api/v1/events?project=PLAT` is a [server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html)
stream for one project (cookie or token auth). Event names are domain events —
`issue.created`, `issue.updated`, `issue.transitioned`, `issue.deleted`, `comment.created`,
`sprint.started`, `sprint.completed`, `release.released`, `board.updated`, development activity
(`pr.opened`, `commit.pushed`, …) — with data `{type, projectId, issueId, issueKey, actorId}`.
Fetch the resource for its new state. A comment line is sent every 25 seconds to keep proxies
from closing the connection.

## Webhooks and SCIM

Inbound: `/hooks/github/{connection}`, `/hooks/gitlab/{connection}` ([scm.md](scm.md)) and
`/scim/v2` ([identity.md](identity.md#scim-20)). Outbound: [automation.md](automation.md#outbound-webhooks).

## Health and metrics

- `GET /healthz` — liveness; `GET /readyz` — readiness (database reachable).
- Prometheus metrics on `WFM_METRICS_ADDR` (`:9090/metrics`): `wfm_http_requests_total`,
  `wfm_http_request_duration_seconds`, `wfm_jobs_total`, `wfm_job_duration_seconds`,
  `wfm_scm_webhooks_received_total`, `wfm_sse_subscribers`, plus Go runtime and process metrics.
- Traces: set `OTEL_EXPORTER_OTLP_ENDPOINT` to export OpenTelemetry traces (HTTP, SQL, jobs).
