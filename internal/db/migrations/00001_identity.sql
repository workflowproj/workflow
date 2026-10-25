-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Users are people or service accounts. Email is always stored lower-case.
CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            text NOT NULL DEFAULT 'human' CHECK (kind IN ('human', 'service')),
    email           text NOT NULL,
    -- SCIM userName; defaults to the email address.
    username        text NOT NULL,
    display_name    text NOT NULL,
    given_name      text,
    family_name     text,
    avatar_url      text,
    org_role        text NOT NULL DEFAULT 'member' CHECK (org_role IN ('admin', 'member', 'guest')),
    active          boolean NOT NULL DEFAULT true,
    -- Which provisioning path created and owns this user.
    source          text NOT NULL CHECK (source IN ('jit', 'scim', 'google', 'admin', 'dev', 'system')),
    -- Google account id. Equal to the Google OIDC `sub` and the Directory API user id.
    google_id       text UNIQUE,
    scim_connection_id uuid,
    scim_external_id text,
    deactivated_at  timestamptz,
    -- Removed upstream (SCIM DELETE / deleted in Google). Kept for history; anonymised after retention.
    deleted_at      timestamptz,
    anonymized_at   timestamptz,
    last_login_at   timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (email);
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
CREATE UNIQUE INDEX users_scim_external_key ON users (scim_connection_id, scim_external_id) WHERE scim_external_id IS NOT NULL;
CREATE INDEX users_display_name_trgm ON users USING gin (display_name gin_trgm_ops);

-- Federated identities; the OIDC client is generic so Entra, Okta or Keycloak can be added beside Google.
CREATE TABLE user_identities (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider     text NOT NULL,
    subject      text NOT NULL,
    email        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    UNIQUE (provider, subject)
);
CREATE INDEX user_identities_user_idx ON user_identities (user_id);

CREATE TABLE sessions (
    -- sha256 of the cookie token; the raw token is never stored.
    id           text PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   timestamptz NOT NULL,
    ip           text,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- Personal access tokens and service-account tokens. Always scoped and expiring.
CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   text NOT NULL UNIQUE,
    token_prefix text NOT NULL,
    scopes       text[] NOT NULL,
    expires_at   timestamptz NOT NULL,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id);

-- One bearer token per IdP that pushes users over SCIM.
CREATE TABLE scim_connections (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name         text NOT NULL,
    token_hash   text NOT NULL UNIQUE,
    token_prefix text NOT NULL,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);
ALTER TABLE users ADD CONSTRAINT users_scim_connection_fk
    FOREIGN KEY (scim_connection_id) REFERENCES scim_connections (id) ON DELETE SET NULL;

CREATE TABLE groups (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name       text NOT NULL,
    description        text NOT NULL DEFAULT '',
    source             text NOT NULL DEFAULT 'local' CHECK (source IN ('local', 'scim', 'google')),
    scim_connection_id uuid REFERENCES scim_connections (id) ON DELETE CASCADE,
    -- SCIM externalId, or the Google group id.
    external_id        text,
    email              text,
    -- Members of the group get at least this organisation role.
    org_role           text CHECK (org_role IN ('admin', 'member', 'guest')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX groups_source_name_key ON groups (source, coalesce(scim_connection_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(display_name));
CREATE UNIQUE INDEX groups_external_key ON groups (source, coalesce(scim_connection_id, '00000000-0000-0000-0000-000000000000'::uuid), external_id) WHERE external_id IS NOT NULL;

CREATE TABLE group_members (
    group_id   uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX group_members_user_idx ON group_members (user_id);

CREATE TABLE directory_sync_runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger     text NOT NULL CHECK (trigger IN ('manual', 'scheduled')),
    status      text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed')),
    stats       jsonb NOT NULL DEFAULT '{}',
    error       text,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

-- Project roles are org-wide definitions holding named permissions; built-ins cannot be deleted.
CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key         text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    permissions text[] NOT NULL,
    builtin     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO roles (key, name, description, permissions, builtin) VALUES
    ('viewer', 'Viewer', 'Read-only access to the project.', ARRAY['project.browse'], true),
    ('member', 'Member', 'Create, edit, transition and comment on issues; run sprints.',
        ARRAY['project.browse', 'issue.create', 'issue.edit', 'issue.edit_others', 'issue.transition', 'issue.assign',
              'issue.comment', 'sprint.manage', 'board.manage'], true),
    ('admin', 'Administrator', 'Full control of the project and its configuration.',
        ARRAY['project.browse', 'issue.create', 'issue.edit', 'issue.edit_others', 'issue.transition', 'issue.assign',
              'issue.comment', 'issue.delete', 'comment.moderate', 'sprint.manage', 'release.manage', 'board.manage',
              'project.admin'], true);

-- Append-only log of security and configuration events.
CREATE TABLE audit_events (
    id          bigserial PRIMARY KEY,
    actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_type  text NOT NULL CHECK (actor_type IN ('user', 'token', 'scim', 'system')),
    action      text NOT NULL,
    target_type text,
    target_id   text,
    project_id  uuid,
    data        jsonb NOT NULL DEFAULT '{}',
    ip          text,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_created_idx ON audit_events (created_at DESC);
CREATE INDEX audit_events_action_idx ON audit_events (action, created_at DESC);

-- +goose Down
DROP TABLE audit_events, roles, directory_sync_runs, group_members, groups, scim_connections, api_tokens, sessions, user_identities, users CASCADE;
