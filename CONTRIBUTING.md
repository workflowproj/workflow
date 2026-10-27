# Contributing to Workflow

This guide is for working on Workflow itself: running it from source, the code generators,
database migrations, the tests and the local Kubernetes loop. To install and use Workflow, see the
[documentation](https://docs.workflowproj.io/) instead.

## Prerequisites

- Go 1.26 or later
- Node 22 or later
- Docker, for PostgreSQL (and Testcontainers, if you don't point the tests at a database)
- For the in-cluster loop: [k3d](https://k3d.io/), [Tilt](https://tilt.dev/) and Helm 3

## Run it from source

```sh
make db-up                       # PostgreSQL 17 on :5432
make dev                         # the API on :8080 with development sign-in (no identity provider needed)
cd web && npm ci && npm run dev  # the UI on http://localhost:5173, proxying /api to :8080
```

Open http://localhost:5173, sign in with any email address (the first account becomes the
organisation admin) and create a project from a template.

To run the production build, with the UI embedded in the binary:

```sh
make web embed-web build
WFM_ENV=development WFM_DEV_LOGIN=true ./bin/wfm serve --migrate   # http://localhost:8080
```

`make help` lists every target.

## Repository layout

```
cmd/wfm/                 the binary: serve, migrate, workflow apply|validate|export, mcp
api/openapi.yaml         the API contract (source of truth for the server and the UI's types)
internal/app/            wiring, modes (web, hooks, worker), the background job registry
internal/httpapi/        HTTP handlers (generated strict server, SSE, webhooks)
internal/tracker/        projects, issues, boards, sprints, releases, the workflow runtime
internal/workflow/       workflow YAML, validation and the CEL evaluator (pure)
internal/search/         the query language: parser and SQL compiler (pure)
internal/identity/       OIDC sign-in, sessions, tokens, SCIM, directory sync, local accounts
internal/scm/            GitHub and GitLab providers, webhook processing, reconciliation
internal/automation/     rules, notifications, outbound webhooks, Slack and Mattermost
internal/servicedesk/    service projects, the customer portal, request types, queues and approvals
internal/sla/            SLA clocks on business-hours calendars (pure)
internal/mcpserver/      the MCP server for AI assistants (tools over the API handlers)
internal/oauth/          the OAuth 2.1 authorisation server for assistants connecting to /mcp
internal/jiraimport/     the importer
internal/db/             migrations (goose) and queries (sqlc)
internal/integration/    tests of the whole API against PostgreSQL
web/                     the React UI (Vite, TanStack Router and Query, dnd-kit, React Flow, Tiptap)
deploy/                  the Helm chart (deploy/helm/wfm) and the local k3d setup (deploy/k3d)
test/scale/              the 100,000-issue scale test
```

[Architecture](docs/architecture.md) explains how the pieces fit together.

## Generated code

The API is defined first: change `api/openapi.yaml`, then regenerate. SQL lives in
`internal/db/queries/*.sql` and sqlc turns it into Go in `internal/db/dbq`.

```sh
make tools       # install the pinned generators (sqlc, oapi-codegen)
make generate    # sqlc, the Go API server and the UI's TypeScript API types
```

Generated files are committed. CI fails if they are out of date (`make generate-check`).

## Database migrations

Migrations are goose SQL files in `internal/db/migrations`, numbered in order
(`00015_service_desk.sql`). In a cluster they run as an Argo CD PreSync job before new pods start,
while the previous version is still serving, so write them expand-then-contract: add columns and
tables first, backfill, and drop what the old version used only in a later release.

## Tests

| Command | What it runs |
| --- | --- |
| `make test-unit` | Go packages that need no database: the workflow engine and CEL, the query language, ranking, passwords and tokens, rate limiting, the outbound request guard |
| `make test-integration` | The whole server against PostgreSQL, one fresh database per test cloned from a migrated template. Set `TEST_DATABASE_URL` (for example `postgres://wfm:wfm@localhost:5432/wfm?sslmode=disable` after `make db-up`), or let it start PostgreSQL with Testcontainers |
| `make lint` | `go vet` and the UI typecheck |
| `cd web && npm test` | Vitest unit tests for the UI |
| `cd web && npm run e2e` | Playwright end-to-end tests against a running server with development sign-in: `make dev` and `npm run dev` locally (or set `E2E_BASE_URL`) |

The end-to-end suite includes an axe audit of every main page in light and dark mode, which fails
on any serious or critical WCAG 2.1 A/AA violation. Identity providers, Git hosts and the importer's
source are tested against fakes of their APIs, so no test needs network access.

Before changing search, boards, reports or metrics, run the [scale test](test/scale/README.md)
before and after: it seeds 100,000 issues and times the API and the heaviest pages.

## Local cluster: k3d and Tilt

```sh
k3d cluster create --config deploy/k3d/cluster.yaml   # k3s and a local registry, Traefik on :8081
tilt up                                               # builds the image, deploys the chart, port-forwards
open http://localhost:8080                            # or http://wfm.localhost:8081 through Traefik
tilt up -- --cnpg                                     # CloudNativePG instead of the throwaway PostgreSQL
tilt down && k3d cluster delete wfm
```

Tilt deploys the same chart with `deploy/k3d/values-dev.yaml` (development sign-in, one replica
of each mode, NetworkPolicies on) and a development Keycloak (below). For UI work, trigger the
`vite` resource: Vite on :5173 proxies `/api` to the port-forwarded web pod.

To check the chart the way CI does:

```sh
helm lint --strict deploy/helm/wfm -f deploy/helm/wfm/values-k3s.yaml \
  --set secrets.values.WFM_SECRET_KEY=dummy-secret-key-0123456789abcdef
helm template wfm deploy/helm/wfm -f deploy/helm/wfm/values-eks.yaml | kubeconform -strict -kubernetes-version 1.33.0 -ignore-missing-schemas
```

### Development Keycloak realm

`deploy/k3d/keycloak` runs Keycloak 26.4 in development mode for the k3d cluster and the CI
end-to-end job, importing the realm `wfm` from `realm-wfm.json` at every start. The admin console
is `http://keycloak.localhost:8081/admin` (`admin` / `admin`). The issuer is
`http://keycloak.localhost:8081/realms/wfm` for browsers (through Traefik) and for pods alike: a
CoreDNS rewrite (`coredns-custom.yaml`) resolves `keycloak.localhost` to the Keycloak Service,
which listens on 8081. `deploy/k3d/values-dev.yaml` configures the app for it; development sign-in
stays on.

| Client | Type | Settings | Used by |
|---|---|---|---|
| `workflow` | confidential, standard flow only | secret `workflow-dev-client-secret`; PKCE S256 required; redirect URIs `http://wfm.localhost:8081/auth/keycloak/callback`, `http://localhost:8080/auth/keycloak/callback`; post-logout redirects to the same hosts; back-channel logout URL `http://wfm-web.wfm.svc.cluster.local/auth/keycloak/backchannel-logout` with *session required*; front-channel logout off; mapper `groups` (group membership, full path, in ID, access and userinfo tokens) | sign-in (`WFM_OIDC_KEYCLOAK_*`) |
| `workflow-sync` | confidential, service account only | secret `workflow-sync-dev-secret`; service account roles `realm-management` `view-users`, `query-groups` | directory sync (`WFM_KEYCLOAK_*`) |
| `workflow-api` | confidential, no flows | none (it names the audience) | `WFM_OIDC_KEYCLOAK_API_AUDIENCE=workflow-api` |
| `workflow-ci` | confidential, service account only | secret `workflow-ci-dev-secret`; audience mapper adding `workflow-api` to access tokens; tokens carry `azp` and `client_id` = `workflow-ci` | API tokens as the `ci-bot` service account (`WFM_OIDC_KEYCLOAK_API_CLIENTS=workflow-ci=ci-bot`) |
| `workflow-cli` | public, password grant and device flow | audience mapper `workflow-api`, `groups` mapper; the password grant is for tests only | people's own API tokens |

| User | Password | Email | Groups | Synced? |
|---|---|---|---|---|
| `alice` | `alice-dev-password` | alice@example.com (verified) | `/workflow-users`, `/wfm-admins` | yes; bootstrap admin |
| `bob` | `bob-dev-password` | bob@example.com (verified) | `/workflow-users`, `/wfm-engineering/wfm-platform` | yes |
| `carol` | `carol-dev-password` | carol@example.com (verified) | `/wfm-engineering` | no: not in the access group |
| `dave` | `dave-dev-password` | dave@example.com (verified) | `/workflow-users` | no: disabled |

The app syncs members of `/workflow-users` every 5 minutes and mirrors the `wfm-*` groups
(`wfm-admins`, `wfm-engineering`, `wfm-engineering/wfm-platform`). Realm settings: SSL required
for external requests only, no self-registration, unique emails, login with email allowed.

To run the sync against a real Keycloak outside the cluster, start one with this realm and run the
opt-in integration test:

```sh
docker run -d --name kc -p 127.0.0.1:8180:8080 -e KC_BOOTSTRAP_ADMIN_USERNAME=admin \
  -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
  -v $PWD/deploy/k3d/keycloak/realm-wfm.json:/opt/keycloak/data/import/realm-wfm.json:ro \
  quay.io/keycloak/keycloak:26.4.7 start-dev --import-realm
WFM_TEST_KEYCLOAK_URL=http://127.0.0.1:8180 go test ./internal/integration -run TestKeycloakRealm
```

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request:

| Job | What runs |
| --- | --- |
| Go | `go mod tidy` and generated code are up to date, `go vet`, and the unit and integration tests with the race detector against a PostgreSQL 17 service |
| Web | Generated API types are up to date, the typecheck, Vitest and the production build |
| Helm | `helm lint` and `kubeconform` (CRDs validated against the datree catalog) for every values overlay |
| Image | The container image builds |
| End to end | An ephemeral k3d cluster with CloudNativePG and the Keycloak realm: the chart is installed and upgraded, the ingress split is checked through Traefik, sign-in redirects to Keycloak, a directory sync provisions the realm's access group, and the Playwright suite runs against it. On pull requests it runs when the `e2e` label is added |

`.github/workflows/security.yml` runs on every push and pull request, and weekly:

| Job | What runs |
| --- | --- |
| Go vulnerabilities | `govulncheck` with the Go release the image is built with: known vulnerabilities in code the binary can reach |
| Go static analysis | `gosec`; a reviewed false positive carries a `#nosec <rule> -- reason` comment |
| CodeQL | Go, TypeScript and the GitHub Actions workflows, with the `security-extended` queries |
| Secrets | `gitleaks` over the whole history; `.gitleaks.toml` allows the test fixtures by path |
| UI dependencies | `npm audit`, failing on anything that ships in the UI bundle |
| Trivy | Vulnerabilities in `go.mod` and `package-lock.json`, misconfigurations in the Dockerfile and the Helm chart, and the built image; accepted findings go in `.trivyignore` with a reason |
| Dependency review | On pull requests, any new dependency with a known high or critical vulnerability |

Findings show under the repository's Security tab. Dependabot (`.github/dependabot.yml`) proposes
grouped weekly updates for Go modules, npm packages, GitHub Actions and the Dockerfile's base
images. Run the quick scans locally with `make security-tools security`.

### Releases

Versions follow [semantic versioning](https://semver.org) and come from
[conventional commit](https://www.conventionalcommits.org) messages on `main` (for a squash merge,
the pull request title):

| Commit | Next version |
|---|---|
| `fix: ...` | patch: 0.1.0 → 0.1.1 |
| `feat: ...` | minor: 0.1.0 → 0.2.0 |
| `feat!: ...` or a `BREAKING CHANGE:` footer | major (minor while the version is 0.x) |
| `docs:`, `chore:`, `ci:`, `refactor:`, `test:` ... | no release on their own |

`.github/workflows/release-please.yml` keeps a release pull request open with the next version,
the `CHANGELOG.md` entries and the chart's `version` and `appVersion`. Merging it tags `vX.Y.Z`,
creates the GitHub release with the changelog, and starts `.github/workflows/release.yml`: a
multi-arch image signed with Cosign (keyless) with an attested SBOM, the Helm chart as a signed
OCI artifact, and offline bundles for amd64 and arm64, attached to the release. To force a
version, add a `Release-As: 1.0.0` footer to a commit.

## Documentation

The reference documentation lives in `docs/`, next to the code it describes; change it in the same
pull request as the behaviour. The documentation site
([workflowproj/workflow-docs](https://github.com/workflowproj/workflow-docs)) copies these pages
with its `scripts/sync_app_docs.py`, so edit them here rather than there.

## Issues

Report bugs, suggest features and point out problems in the docs through the forms under
[New issue](https://github.com/workflowproj/workflow/issues/new/choose): they ask for what's needed
to act on it (the version, the steps, the area). Report security vulnerabilities privately, through
the link on that page, rather than in a public issue.

For a large change, open an issue to discuss it before starting on a pull request.

## Pull requests

- Keep each pull request to one change, with its tests and documentation.
- Run `make lint`, `make test` and `cd web && npm test` before pushing (and `make security` when
  you add a dependency or touch sign-in, tokens or outbound requests); add the `e2e` label when
  the change touches the UI, sign-in or the chart.
- Commit generated code with the change that caused it.
