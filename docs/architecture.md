# Architecture

Workflow is a modular monolith: one Go binary, one PostgreSQL database.

```
                    ┌──────────────────────────── PostgreSQL 17 ───────────────────────────┐
 browser ──HTTPS──▶ │ wfm --mode=web     REST API, UI, SSE ───────┐                        │
                    │                                             │  tables, FTS, pg_trgm  │
 GitHub/GitLab ───▶ │ wfm --mode=hooks   verify + store webhooks ─┼─▶ River job queue      │
 IdP (SCIM) ──────▶ │                    SCIM 2.0                 │   LISTEN/NOTIFY        │
                    │ wfm --mode=worker  jobs: webhooks, automation, migrations, sync    │
                    └──────────────────────────────────────────────────────────────────────┘
```

## Modes

`wfm serve --mode=…` selects what a process does. All modes share the same code and image.

| Mode | Serves | Scales on |
|---|---|---|
| `web` | `/api/v1`, `/auth/*`, the UI, `/api/v1/events` (SSE), health | HTTP load (HPA on CPU) |
| `hooks` | `POST /hooks/{provider}/{connection}`, `/scim/v2` | inbound bursts; small and isolated so a webhook storm cannot starve users |
| `worker` | background jobs (River) and periodic schedules | queue depth (KEDA PostgreSQL scaler) |
| `all` | everything (development and small installs) | — |

Metrics are served on a separate port (`WFM_METRICS_ADDR`, default `:9090`) in every mode.

## Request path

The API is defined first in `api/openapi.yaml`. `oapi-codegen` generates a strict Go server
interface (`internal/api`) that `internal/httpapi` implements, and `openapi-typescript`
generates the UI's types. Errors are RFC 9457 `application/problem+json` with a machine-readable
`code` (e.g. `version_conflict`, `screen_required`, `status_mapping_required`).

Authentication (`internal/identity`) accepts a session cookie (from OIDC sign-in) or a bearer
token (personal access token or service account). Cookie requests that change state must carry
`X-Requested-With` (CSRF). Authorisation (`internal/authz`) resolves the caller's organisation
role, then per project the union of permissions from direct membership and group memberships —
or, for open projects without an explicit membership, the project's open role.

## Domain

`internal/tracker` owns projects, issues and planning. Notable choices:

- **Issue keys**: a per-project counter incremented under `SELECT … FOR UPDATE`; renamed project
  keys are kept as aliases so old links and commit messages still resolve.
- **Ranking**: fractional indexing strings compared with `COLLATE "C"`, so a drag-and-drop
  touches one row.
- **Optimistic locking**: every issue has a `version`; updates with a stale version answer 409.
- **Custom fields** live in a JSONB column validated against field definitions.
- **Search**: `internal/search` parses JQL and compiles it to parameterised SQL. Text search uses
  a `tsvector` expression index plus `pg_trgm` for fuzzy matching. Results are always limited
  to projects the caller may browse.

## Workflows

`internal/workflow` parses YAML into a definition, validates it (unknown or duplicate statuses,
transitions, CEL compile errors, screen fields, post-functions) and evaluates CEL with a cost limit. Workflows are
versioned and immutable; issues pin the version they were created or migrated under. Publishing
a version that removes statuses requires a mapping, and a `workflow_migrate` job moves issues to
the new version in batches (`FOR UPDATE SKIP LOCKED`). See [workflows.md](workflows.md).

Transitions run in one database transaction: condition → screen → validators → status change →
resolution → in-database post-functions (assign, set or clear a field) → history → domain event.
Post-functions with side effects outside the database (notify, webhook) are enqueued as jobs in
the same transaction.

## Events, jobs and live updates

Every change writes a domain event inside its transaction in two forms:

1. a River job (`domain_event`) inserted with `InsertTx` — the outbox pattern, so automation,
   notifications and outbound webhooks run if and only if the change committed;
2. a `pg_notify` on commit, which the web process fans out to subscribed browsers over SSE.

Job kinds: `domain_event`, `scm_webhook`, `scm_writeback`, `scm_reconcile`, `scm_ensure_hook`,
`workflow_migrate`, `post_function`, `webhook_deliver`, `directory_sync`,
`automation_schedule`, `maintenance`. Failed jobs retry with backoff. Periodic jobs: scheduled
automation (every minute), maintenance (hourly: expired sessions, anonymising users deleted
upstream after the retention period, pruning webhook payloads), SCM reconcile and directory
sync on their configured intervals.

## Integrations

- **SCM** (`internal/scm`): a `Provider` interface implemented for GitHub (App JWT →
  installation tokens) and GitLab (access token). Webhooks are verified (HMAC / token), stored
  with a unique delivery id, and processed asynchronously. Keys are only linked for projects
  connected to the repository. See [scm.md](scm.md).
- **Identity** (`internal/identity`): OIDC with PKCE and nonce; SCIM 2.0 server; Google Admin SDK
  directory sync via domain-wide delegation. See [identity.md](identity.md).
- **Outbound webhooks** go through an SSRF-guarded dialer that refuses private, loopback and
  link-local addresses unless explicitly allowed.

## Storage

PostgreSQL holds everything, including attachments when no S3 bucket is configured (fine for
small installs; configure `WFM_S3_*` for anything larger). Migrations are embedded (goose) and
run by `wfm migrate` as a Kubernetes Job: a Helm post-install/pre-upgrade hook, or an Argo CD
sync hook ordered before the Deployments (see [deployment.md](deployment.md)).

## Front end

`web/` is a React + TypeScript single-page app built with Vite and embedded into the binary
(`internal/webui`). TanStack Router (code-based routes) and TanStack Query; dnd-kit for boards
and backlogs; React Flow for the workflow editor; Tiptap for Markdown editing. Live updates
invalidate cached queries when SSE events arrive.
