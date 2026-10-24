# Configuration

`wfm` is configured with environment variables. Durations use Go syntax (`15m`, `24h`); lists
are comma-separated. Invalid configuration stops the process at startup with every problem listed.

## Core

| Variable | Default | |
|---|---|---|
| `WFM_MODE` | `all` | `web`, `hooks`, `worker` or `all` (also `wfm serve --mode=`) |
| `WFM_ENV` | `production` | anything else enables development conveniences and relaxes `WFM_SECRET_KEY` |
| `WFM_DATABASE_URL` | `postgres://wfm:wfm@localhost:5432/wfm?sslmode=disable` | PostgreSQL 16+ (17 recommended) |
| `WFM_PUBLIC_URL` | `http://localhost:8080` | external URL of the UI; used for OIDC redirects and links |
| `WFM_HOOKS_PUBLIC_URL` | `WFM_PUBLIC_URL` | external URL where `/hooks` and `/scim` are reachable |
| `WFM_SECRET_KEY` | — | **required in production**; 32+ random bytes; encrypts stored secrets (SCM keys, webhook secrets) and signs cookies. Keep it stable — changing it makes stored secrets unreadable |
| `WFM_HTTP_ADDR` | `:8080` | |
| `WFM_METRICS_ADDR` | `:9090` | Prometheus `/metrics` |
| `WFM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `WFM_LOG_FORMAT` | `json` | `json` or `text` |
| `WFM_SESSION_TTL` | `168h` | browser session lifetime |
| `WFM_DEV_LOGIN` | `false` | passwordless sign-in for development; refused when `WFM_ENV=production` |

## Request limits

Requests to the API, sign-in, MCP, SCIM and webhooks are limited per process (with several
replicas, multiply by the replica count). Over a limit the server answers 429 with `Retry-After`
and counts it in `wfm_rate_limited_total`. Limits are on by default when `WFM_ENV=production` and
off otherwise; each value is requests per minute, with bursts of half that, and `0` turns a
limit off.

| Variable | Default (production) | |
|---|---|---|
| `WFM_RATE_LIMIT_PER_MINUTE` | `600` | per access token or browser session |
| `WFM_RATE_LIMIT_ANONYMOUS_PER_MINUTE` | `120` | per client address, requests without credentials (sign-in pages, metadata) |
| `WFM_RATE_LIMIT_ADDRESS_PER_MINUTE` | `3000` | per client address, all requests (offices behind one address share it) |
| `WFM_RATE_LIMIT_HOOKS_PER_MINUTE` | `1200` | per webhook endpoint (`/hooks/...`) |
| `WFM_TRUSTED_PROXIES` | private and loopback ranges | CIDRs of proxies allowed to set `X-Forwarded-For` (the ingress controller, a load balancer). Requests from anywhere else are their own client, whatever the header says |

## Sign-in and provisioning

See [identity.md](identity.md).

| Variable | Default | |
|---|---|---|
| `WFM_GOOGLE_CLIENT_ID`, `WFM_GOOGLE_CLIENT_SECRET` | — | enable Google sign-in |
| `WFM_OIDC_PROVIDERS` | `google` if a Google client id is set | provider ids, e.g. `google,okta` |
| `WFM_OIDC_<ID>_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET` | — | per provider (Google's issuer is preset) |
| `WFM_OIDC_<ID>_DISPLAY_NAME`, `_SCOPES`, `_ALLOWED_DOMAINS` | | optional per provider |
| `WFM_ALLOWED_DOMAINS` | — | default allowed domains for every provider |
| `WFM_JIT_PROVISIONING` | `auto` | `auto`, `true`, `false` |
| `WFM_BOOTSTRAP_ADMINS` | — | emails that are always organisation admins |
| `WFM_DELETED_USER_RETENTION` | `2160h` (90 days) | then deleted users are anonymised |
| `WFM_GOOGLE_DIRECTORY_ENABLED` | `false` | Google Workspace directory sync |
| `WFM_GOOGLE_SERVICE_ACCOUNT_KEY` | — | path, JSON or base64 JSON |
| `WFM_GOOGLE_ADMIN_SUBJECT` | — | admin to impersonate (domain-wide delegation) |
| `WFM_GOOGLE_CUSTOMER` | `my_customer` | |
| `WFM_GOOGLE_SYNC_GROUP` | — | access group email |
| `WFM_GOOGLE_SYNC_ORG_UNIT` | — | org unit path, e.g. `/Engineering` |
| `WFM_GOOGLE_SYNC_INTERVAL` | `15m` | |
| `WFM_GOOGLE_SYNC_GROUPS` | `true` | mirror groups |
| `WFM_GOOGLE_GROUP_PREFIX` | — | only mirror groups whose email starts with this |

## AI assistants (OAuth)

Assistants connect to `/mcp` by URL and sign in through the browser; see [mcp.md](mcp.md).
`WFM_PUBLIC_URL` is the OAuth issuer and `<WFM_PUBLIC_URL>/mcp` the token audience, so it must be
the address assistants use (https in production).

| Variable | Default | |
|---|---|---|
| `WFM_OAUTH_ENABLED` | `true` | `false`: no OAuth; `/mcp` accepts personal access tokens only |
| `WFM_OAUTH_DYNAMIC_REGISTRATION` | `true` | allow apps to register themselves at `/oauth/register` (RFC 7591); `false` leaves client ID metadata documents |
| `WFM_OAUTH_CLIENT_METADATA_DOCUMENTS` | `true` | accept https client ids that point at a client ID metadata document (fetched under the outbound guard below) |
| `WFM_OAUTH_ACCESS_TOKEN_TTL` | `1h` | access token lifetime (1m–24h) |
| `WFM_OAUTH_REFRESH_TOKEN_TTL` | `720h` (30 days) | refresh token lifetime, renewed on every refresh (1h–8784h) |

## Attachments

Without S3, attachments are stored in PostgreSQL.

| Variable | Default | |
|---|---|---|
| `WFM_S3_ENDPOINT` | — | e.g. `s3.amazonaws.com`, `minio.storage:9000` |
| `WFM_S3_BUCKET` | — | |
| `WFM_S3_REGION` | `us-east-1` | |
| `WFM_S3_ACCESS_KEY`, `WFM_S3_SECRET_KEY` | — | omit to use the environment's credentials (e.g. IRSA) |
| `WFM_S3_USE_SSL` | `true` | |
| `WFM_ATTACHMENT_MAX_MB` | `25` | |

## Time tracking

Durations such as `1w 2d 3h` count working days and weeks; see [time-tracking.md](time-tracking.md).

| Variable | Default | |
|---|---|---|
| `WFM_TIME_HOURS_PER_DAY` | `8` | hours in a working day (`1d`); more than 0, at most 24 |
| `WFM_TIME_DAYS_PER_WEEK` | `5` | working days in a week (`1w`); more than 0, at most 7 |

## Background work and integrations

| Variable | Default | |
|---|---|---|
| `WFM_WORKER_CONCURRENCY` | `10` | jobs per queue per worker process |
| `WFM_SCM_RECONCILE_INTERVAL` | `30m` | `0` disables |
| `WFM_AUTOMATION_MAX_DEPTH` | `5` | maximum chain of rules triggering rules |
| `WFM_WEBHOOK_ALLOW_PRIVATE` | `false` | allow outbound calls to any private address (prefer the allowlist below) |
| `WFM_OUTBOUND_ALLOWED_HOSTS` | — | host names (or `.domain` suffixes) that may resolve to private addresses, e.g. a self-hosted Mattermost |

## Email

Without `WFM_SMTP_HOST`, email is not sent: messages are logged and kept in memory (in
development they can be read at `/api/v1/dev/mailbox`). Email is queued after the change that
causes it and retried on failure.

| Variable | Default | |
|---|---|---|
| `WFM_SMTP_HOST` | — | SMTP server, e.g. `smtp.sendgrid.net`, `email-smtp.eu-west-1.amazonaws.com` |
| `WFM_SMTP_PORT` | `587` | |
| `WFM_SMTP_TLS` | `starttls` | `starttls`, `tls` (implicit, usually port 465) or `none` |
| `WFM_SMTP_USERNAME`, `WFM_SMTP_PASSWORD` | — | omit for an unauthenticated relay |
| `WFM_SMTP_FROM` | `Workflow <workflow@localhost>` | sender address |

## Slack and Mattermost

See [chat.md](chat.md). Channel notifications need no configuration; these enable the `/wfm` command.

| Variable | Default | |
|---|---|---|
| `WFM_SLACK_SIGNING_SECRET` | — | Slack app signing secret; enables `/hooks/chat/slack` |
| `WFM_MATTERMOST_COMMAND_TOKENS` | — | slash command token(s); enables `/hooks/chat/mattermost` |
| `WFM_CHAT_COMMAND_PROJECTS` | every open project | project keys the command may read |

## Telemetry

Standard OpenTelemetry variables: `OTEL_EXPORTER_OTLP_ENDPOINT` (or
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`) enables trace export; `OTEL_EXPORTER_OTLP_HEADERS`,
`OTEL_TRACES_SAMPLER` and friends apply.

## CLI

`wfm workflow …` reads `WFM_SERVER` (base URL) and `WFM_TOKEN` (a token with the write scope).
