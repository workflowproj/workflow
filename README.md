# Workflow

Workflow is a free, open-source issue tracker for software teams that runs on your own
infrastructure: one container image, PostgreSQL and a Helm chart, on k3s or EKS, including networks
with no internet access. It covers what a team tracks day to day, from the backlog to the pull
request, plus a service desk for requests from outside the team.

**Documentation: [docs.workflowproj.io](https://docs.workflowproj.io/)** · Website:
[workflowproj.io](https://workflowproj.io/)

## What it does

- **Projects and issues**: project keys and templates; epics, stories, tasks, bugs and sub-tasks;
  custom fields, links, attachments, watchers and a full history.
- **Workflows as code**: versioned YAML with CEL conditions and validators, transition screens and
  post-functions, edited visually or applied from CI.
- **Boards and planning**: Scrum and Kanban boards with WIP limits and swimlanes, a ranked backlog,
  sprints, releases and a timeline across projects, updated live.
- **Releases**: a page per release with progress, a burnup and readiness checks before you ship,
  release notes generated from done issues and published to the help centre, Git tags and the
  environments a release reached.
- **Risks and decisions**: a risk register scored by likelihood × impact with a heat map, and a
  decision log with ADR export, in their own project tabs and out of sprint planning; work links
  to the risks it mitigates and the decisions it implements.
- **Search**: a JQL-like query language compiled to SQL, with saved and shared filters.
- **Reports and dashboards**: lead and cycle time, burndown and burnup, velocity, cumulative flow
  and breakdowns, on personal and shared dashboards.
- **Time tracking**: estimates, work logs, a burndown in hours and a timesheet.
- **Service desk**: service projects with a help centre for customers, request forms, queues, SLAs
  on business-hours calendars and approvals.
- **Automation**: rules on issue events, Git activity or a schedule, and signed outbound webhooks.
- **Git**: GitHub and GitLab (cloud or self-managed) linked to issues by key, with smart commits,
  pull and merge requests, pipeline status, tags and deployments.
- **Identity**: sign-in with any OpenID Connect provider, groups from sign-in tokens, single and
  back-channel logout, directory sync from Google Workspace or Keycloak, SCIM 2.0, and local
  accounts with two-factor authentication.
- **Notifications**: in the app and by email, immediately or in a daily digest, plus Slack and
  Mattermost channels.
- **API and AI assistants**: a REST API (OpenAPI 3.1), access tokens, and an MCP server so Claude
  and other assistants can work with issues as you.
- **Import from Jira**: Cloud or Data Center projects with their history, re-runnable while teams
  move across.

## How it runs

- One image in three modes, `web`, `hooks` and `worker`, so webhook bursts and background jobs
  scale apart from the UI. PostgreSQL is the only stateful dependency: the job queue, search and
  live updates run on it, and attachments can too.
- One Helm chart for k3s and EKS, with CloudNativePG, autoscaling (HPA and KEDA), NetworkPolicies,
  Ingress or Gateway API, External Secrets and Argo CD hooks for migrations.
- Only the `hooks` endpoint (Git webhooks, SCIM) needs to face the internet. Offline bundles cover
  air-gapped installs, and releases are signed with Cosign and carry SBOMs.
- Prometheus metrics with a Grafana dashboard and alert rules, OpenTelemetry traces and JSON logs.
  Searches, boards and reports are measured against 100,000 issues.

## Get started

- **Install it on a cluster**: [Getting started](https://docs.workflowproj.io/getting-started/)
  and [Deployment](docs/deployment.md).
- **Run it from source**: see [Contributing](CONTRIBUTING.md#run-it-from-source).

## Documentation

The reference pages live in [`docs/`](docs/) and are published at
[docs.workflowproj.io](https://docs.workflowproj.io/).

| Topic | |
|---|---|
| [Architecture](docs/architecture.md) | Components, modes, data flow, the job queue and live updates |
| [Deployment](docs/deployment.md) | Kubernetes with Helm, upgrades, backups, air-gapped installs |
| [Configuration](docs/configuration.md) | Every `WFM_*` environment variable |
| [Sign-in and provisioning](docs/identity.md) | OpenID Connect, SCIM, Google and Keycloak directory sync, local accounts, roles and tokens |
| [Workflows as code](docs/workflows.md) | The YAML format, CEL reference, versioning and migration |
| [Releases](docs/releases.md) | Release pages, readiness, release notes and What's new, tags and deployments |
| [Search](docs/search.md) | The query language |
| [Risks and decisions](docs/risks-decisions.md) | The risk register, the decision log, scoring and their links |
| [Metrics and dashboards](docs/metrics.md) | Issue timing, sprint reports, flow metrics, dashboards |
| [Time tracking](docs/time-tracking.md) | Estimates, logging work, the hours burndown and the timesheet |
| [Service desk](docs/service-desk.md) | Help centre, customers, request types, queues, SLAs and approvals |
| [Automation and webhooks](docs/automation.md) | Rules, actions, outbound webhook signatures |
| [GitHub and GitLab](docs/scm.md) | Connecting a GitHub App or GitLab, smart commits, write-back, tags and deployments |
| [Notifications and email](docs/notifications.md) | Email preferences, immediate emails, daily digests, unsubscribe |
| [Slack and Mattermost](docs/chat.md) | Channel notifications and the `/wfm` command |
| [AI assistants (MCP)](docs/mcp.md) | Connecting Claude and other assistants; the tools |
| [API](docs/api.md) | REST API, authentication, errors, live events |
| [Import from Jira](docs/import.md) | Moving projects across with their history; placeholders for people |

## Contributing

Bug reports, ideas and pull requests are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) covers running
Workflow from source, the code generators, migrations, the tests and the local Kubernetes loop.

## Licence

Workflow is free and open source under the [Apache License 2.0](LICENSE); see [NOTICE](NOTICE)
for attributions. There is one edition, with every feature: no paid tier, no feature gates and no
licence keys.

Jira is a trademark of Atlassian. GitHub, GitLab, Slack, Mattermost, Google and other names are
trademarks of their respective owners. Workflow is not affiliated with, endorsed or sponsored by
any of them; their names are used only to describe compatibility.
