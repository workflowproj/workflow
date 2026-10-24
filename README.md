# Workflow

A self-hosted, Kubernetes-native work tracker for software teams: projects, epics, stories,
tasks, bugs and sub-tasks; boards, backlogs, sprints and releases; versioned workflows-as-code;
a JQL-style query language; GitHub and GitLab integration; Google sign-in with SCIM and Google
directory provisioning.

It is one Go binary (`wfm`) and PostgreSQL. Nothing else is required: the job queue, full-text
search and live updates all run on Postgres.

## Features

- **Projects** from templates (Scrum, Kanban, Bug tracking), open or private, with key renames
  that keep old keys working.
- **Issue hierarchy** by level: epics (1) → stories, tasks, bugs (0) → sub-tasks (−1). Custom
  issue types, custom fields, labels, links, watchers, attachments, comments (Markdown) and full
  history.
- **Workflows as code.** YAML documents with statuses (todo / in progress / done), transitions,
  CEL conditions and validators, screens and post-functions. Every change publishes a new
  version; issues stay on their version until a background job migrates them with your status
  mapping. Edit visually in the browser or apply from CI with `wfm workflow apply`.
- **Boards and planning.** Scrum and Kanban boards with columns you edit right on the board,
  configurable cards, swimlanes, WIP limits and quick filters, including assignee avatars to show
  one or several people's work; ranked backlog; sprints; releases.
- **JQL search** compiled to SQL, with autocomplete, saved and shared filters.
- **GitHub and GitLab.** A GitHub App or GitLab token per connection; verified, de-duplicated
  webhooks; branches, commits, pull/merge requests and pipelines linked to issues by key; smart
  commits (`PLAT-12 #start-work #comment ready`) that go through the workflow engine; a comment
  on each new PR/MR; create branches from issues; periodic reconciliation for missed deliveries.
- **Identity.** Google (or any OIDC provider) sign-in with domain restriction; SCIM 2.0 for Okta,
  Entra ID and others; Google Workspace directory sync; just-in-time provisioning when neither is
  used. Organisation roles, project roles, custom roles, groups mapped to roles, personal access
  tokens and service accounts with scopes and expiry, and an audit log.
- **Metrics and dashboards.** Lead time, cycle time and time in status for every issue; scope-aware
  sprint burndown and burnup, velocity, cumulative flow, created vs resolved, cycle time and
  breakdowns; personal and shared dashboards built from gadgets.
- **Time tracking.** Original and remaining estimates, work logged as `1w 2d 3h` against a
  configurable working day, overruns at a glance, JQL on estimates and worklogs, a sprint burndown
  in hours and a timesheet with CSV export.
- **Timeline.** A roadmap per project and across all projects: epics and their issues on a time
  axis, dependency arrows (with conflicts highlighted), sprints and releases; drag to reschedule.
- **Automation.** Rules triggered by issue events, development activity or a schedule, with CEL
  conditions and actions (transition, assign, set field, comment, label, webhook, notify, chat,
  roll up dates) on the issue, its parent or its children; ready-made templates; signed outbound
  webhooks.
- **API and AI assistants.** The REST API behind every screen (OpenAPI published by the server)
  and an MCP server, so Claude and other assistants can search, file, update and move issues and
  read sprint metrics as you.
- **Slack and Mattermost.** Channel notifications with event and JQL filters, a chat action for
  automation, and a read-only `/wfm` command for issue lookup and search.
- **Live.** Boards and issue views update in real time over server-sent events; light and dark
  themes.
- **Operable.** `--mode=web|hooks|worker` from one image, Prometheus metrics, OpenTelemetry
  traces, JSON logs, a Helm chart with CloudNativePG, HPA/KEDA, NetworkPolicies and Argo CD hooks.

## Quick start (local)

Requirements: Go 1.26+, Node 22+, Docker (for PostgreSQL).

```sh
make db-up                     # PostgreSQL 17 on :5432
make dev                       # API on :8080 with development sign-in (no IdP needed)
cd web && npm ci && npm run dev  # UI on http://localhost:5173
```

Open http://localhost:5173, sign in with any email (the first account becomes the
organisation admin) and create a project from a template.

To run the production build locally: `make web embed-web build` then
`WFM_ENV=development WFM_DEV_LOGIN=true ./bin/wfm serve --migrate` and open http://localhost:8080.

## Documentation

| Topic | |
|---|---|
| [Architecture](docs/architecture.md) | Components, modes, data flow, the job queue and live updates |
| [Configuration](docs/configuration.md) | Every `WFM_*` environment variable |
| [Deployment](docs/deployment.md) | Kubernetes with Helm, upgrades, backups, air-gapped installs |
| [Sign-in and provisioning](docs/identity.md) | Google / OIDC, SCIM, Google directory sync, roles and tokens |
| [GitHub and GitLab](docs/scm.md) | Connecting a GitHub App or GitLab, smart commits, write-back |
| [Workflows as code](docs/workflows.md) | The YAML format, CEL reference, versioning and migration |
| [JQL](docs/jql.md) | Query language reference |
| [Automation and webhooks](docs/automation.md) | Rules, actions, outbound webhook signatures |
| [Slack and Mattermost](docs/chat.md) | Channel notifications and the `/wfm` command |
| [Metrics and dashboards](docs/metrics.md) | Issue timing, sprint reports, flow metrics, dashboards |
| [Time tracking](docs/time-tracking.md) | Estimates, logging work, the hours burndown and the timesheet |
| [AI assistants (MCP)](docs/mcp.md) | Connecting Claude and other assistants; the tools |
| [API](docs/api.md) | REST API, authentication, errors, live events |

The REST API is described by [`api/openapi.yaml`](api/openapi.yaml) (OpenAPI 3.1); the Go
server and the TypeScript client are generated from it.

## Development

```sh
make generate          # sqlc, oapi-codegen and the TypeScript API types
make test-unit         # pure Go packages
make test-integration  # whole app against PostgreSQL (TEST_DATABASE_URL or Testcontainers)
make lint              # go vet + web typecheck
cd web && npm test     # web unit tests
```

Layout:

```
cmd/wfm/                 the binary: serve, migrate, workflow apply|validate|export, mcp
api/openapi.yaml         API contract (source of truth)
internal/app/            wiring, modes, background job registry
internal/httpapi/        HTTP handlers (generated strict server + SSE + hooks)
internal/tracker/        projects, issues, boards, sprints, releases, workflow runtime
internal/workflow/       workflow YAML, validation and the CEL evaluator (pure)
internal/search/         JQL parser and SQL compiler (pure)
internal/identity/       OIDC sign-in, sessions, tokens, SCIM server, Google directory sync
internal/scm/            GitHub and GitLab providers, webhook processing, reconcile
internal/automation/     rules, notifications, outbound webhooks, Slack and Mattermost
internal/mcpserver/      the MCP server for AI assistants (tools over the API handlers)
internal/db/             migrations (goose) and queries (sqlc)
internal/integration/    end-to-end tests of the API against PostgreSQL
web/                     React UI (Vite, TanStack Router and Query, dnd-kit, React Flow, Tiptap)
deploy/                  Helm chart and local k3d setup
```
