-- +goose Up
-- A GitHub App or a GitLab instance (gitlab.com or self-managed).
CREATE TABLE scm_connections (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider     text NOT NULL CHECK (provider IN ('github', 'gitlab')),
    name         text NOT NULL,
    -- API base URL, e.g. https://api.github.com, https://ghe.example.com/api/v3, https://gitlab.example.com
    api_url      text NOT NULL,
    web_url      text NOT NULL,
    -- GitHub App id / slug (GitHub only).
    app_id       bigint,
    app_slug     text,
    -- AES-GCM encrypted JSON: {"privateKey": ..., "webhookSecret": ..., "token": ...}.
    secrets_enc  text NOT NULL,
    -- GitLab Free has no group hooks, so project hooks are created through the API.
    manage_hooks boolean NOT NULL DEFAULT false,
    -- Post a comment on PRs/MRs that link an issue; set commit status / check run.
    write_back   boolean NOT NULL DEFAULT true,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    last_event_at timestamptz
);

CREATE TABLE scm_repositories (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id   uuid NOT NULL REFERENCES scm_connections (id) ON DELETE CASCADE,
    -- Provider repository / project id.
    external_id     text NOT NULL,
    full_path       text NOT NULL,
    web_url         text NOT NULL,
    default_branch  text NOT NULL DEFAULT 'main',
    installation_id bigint,
    hook_id         text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (connection_id, external_id)
);
CREATE INDEX scm_repositories_path_idx ON scm_repositories (connection_id, lower(full_path));

-- Only keys of projects connected to a repository are linked, to avoid false positives.
CREATE TABLE project_repositories (
    project_id    uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES scm_repositories (id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, repository_id)
);
CREATE INDEX project_repositories_repo_idx ON project_repositories (repository_id);

-- Webhooks are verified and stored on receipt, then processed asynchronously and idempotently.
CREATE TABLE webhook_events (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id uuid NOT NULL REFERENCES scm_connections (id) ON DELETE CASCADE,
    provider      text NOT NULL,
    event_type    text NOT NULL,
    delivery_id   text NOT NULL,
    payload       jsonb NOT NULL,
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processed', 'ignored', 'failed')),
    error         text,
    attempts      integer NOT NULL DEFAULT 0,
    received_at   timestamptz NOT NULL DEFAULT now(),
    processed_at  timestamptz,
    UNIQUE (connection_id, delivery_id)
);
CREATE INDEX webhook_events_received_idx ON webhook_events (connection_id, received_at DESC);

CREATE TABLE dev_links (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id      uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES scm_repositories (id) ON DELETE CASCADE,
    kind          text NOT NULL CHECK (kind IN ('branch', 'commit', 'pull_request', 'pipeline')),
    -- Branch name, commit sha, PR/MR number or pipeline id.
    external_id   text NOT NULL,
    title         text NOT NULL,
    url           text NOT NULL DEFAULT '',
    -- PR: open | draft | merged | closed. Branch: active | deleted. Pipeline: pending | running | success | failed | canceled.
    state         text NOT NULL DEFAULT '',
    author_name   text NOT NULL DEFAULT '',
    author_email  text NOT NULL DEFAULT '',
    -- Review state, head branch, sha, pipeline name, write-back markers.
    data          jsonb NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issue_id, repository_id, kind, external_id)
);
CREATE INDEX dev_links_repo_idx ON dev_links (repository_id, kind, external_id);

CREATE TABLE automation_rules (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name           text NOT NULL,
    description    text NOT NULL DEFAULT '',
    enabled        boolean NOT NULL DEFAULT true,
    trigger_type   text NOT NULL,
    trigger_config jsonb NOT NULL DEFAULT '{}',
    -- Optional CEL expression over the triggering issue/event.
    condition      text NOT NULL DEFAULT '',
    actions        jsonb NOT NULL DEFAULT '[]',
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    last_run_at    timestamptz,
    next_run_at    timestamptz
);
CREATE INDEX automation_rules_project_idx ON automation_rules (project_id, trigger_type) WHERE enabled;
CREATE INDEX automation_rules_schedule_idx ON automation_rules (next_run_at) WHERE enabled AND trigger_type = 'schedule';

CREATE TABLE automation_runs (
    id         bigserial PRIMARY KEY,
    rule_id    uuid NOT NULL REFERENCES automation_rules (id) ON DELETE CASCADE,
    issue_id   uuid REFERENCES issues (id) ON DELETE SET NULL,
    status     text NOT NULL CHECK (status IN ('success', 'skipped', 'failed')),
    message    text NOT NULL DEFAULT '',
    depth      integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX automation_runs_rule_idx ON automation_runs (rule_id, id DESC);

-- Outbound webhooks. project_id NULL = organisation-wide subscription.
CREATE TABLE webhook_subscriptions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL,
    url        text NOT NULL,
    secret_enc text NOT NULL,
    events     text[] NOT NULL,
    active     boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES webhook_subscriptions (id) ON DELETE CASCADE,
    event           text NOT NULL,
    payload         jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
    attempts        integer NOT NULL DEFAULT 0,
    response_status integer,
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    delivered_at    timestamptz
);
CREATE INDEX webhook_deliveries_sub_idx ON webhook_deliveries (subscription_id, created_at DESC);

-- +goose Down
DROP TABLE webhook_deliveries, webhook_subscriptions, automation_runs, automation_rules, dev_links, webhook_events, project_repositories, scm_repositories, scm_connections CASCADE;
