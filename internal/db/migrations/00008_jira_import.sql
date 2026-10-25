-- +goose Up
-- Imports from Jira Cloud (REST v3) or Jira Data Center / Server (REST v2), run from
-- Administration → Import from Jira. One row per connected site and wizard; the API token is
-- sealed with the server key (secure.Box) and can be deleted once the import is done.
CREATE TABLE jira_imports (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    flavor           text NOT NULL CHECK (flavor IN ('cloud', 'datacenter')),
    base_url         text NOT NULL,
    -- The site in jira_import_map: imports of the same site share the mapping and never duplicate.
    site             text NOT NULL,
    -- Atlassian account email (Cloud basic auth); empty for Data Center personal access tokens.
    email            text NOT NULL DEFAULT '',
    token_enc        text,
    server_title     text NOT NULL DEFAULT '',
    status           text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'scanning', 'scanned', 'running', 'succeeded', 'failed', 'cancelled')),
    -- Selected projects with their targets, and the value mappings (statuses, types, priorities,
    -- story points field, people).
    config           jsonb NOT NULL DEFAULT '{}',
    -- Analysis of the selected projects: counts, values in use, people, oversize attachments.
    scan             jsonb,
    -- Phases with done/total counters, counters and problems of the last scan or import run.
    progress         jsonb,
    -- Where the running job resumes: phase, project and the last Jira issue id written.
    cursor           jsonb NOT NULL DEFAULT '{}',
    -- The current run; a job carrying another run id stops without writing.
    run_id           uuid,
    mode             text CHECK (mode IN ('scan', 'import')),
    cancel_requested boolean NOT NULL DEFAULT false,
    error            text,
    created_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    finished_at      timestamptz,
    heartbeat_at     timestamptz
);
CREATE INDEX jira_imports_site_idx ON jira_imports (site);

-- Jira ids to ids here, per site, so a re-run updates or skips instead of duplicating and an
-- interrupted run resumes. data keeps what later phases need (parent, links, rank, the Jira
-- "updated" time and the mapping fingerprint used to skip unchanged issues).
CREATE TABLE jira_import_map (
    site       text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('project', 'user', 'issue', 'attachment', 'sprint', 'version')),
    jira_id    text COLLATE "C" NOT NULL,
    local_id   uuid NOT NULL,
    project_id uuid REFERENCES projects (id) ON DELETE CASCADE,
    import_id  uuid REFERENCES jira_imports (id) ON DELETE SET NULL,
    data       jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (site, kind, jira_id)
);
CREATE INDEX jira_import_map_local_idx ON jira_import_map (kind, local_id);
CREATE INDEX jira_import_map_project_idx ON jira_import_map (project_id, kind, jira_id) WHERE project_id IS NOT NULL;

-- People known only from an import: Jira users with no account here. They own history,
-- comments and assignments, but the user row is inactive and has no credentials, so nobody can
-- sign in as them. Signing in with the same email through SSO (when just-in-time provisioning
-- is allowed), Google directory sync or SCIM activates the row - the person "claims" it - and
-- the trigger below drops the placeholder marker.
CREATE TABLE user_placeholders (
    user_id     uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Where the person came from ('jira') and their id there (account id or user key).
    source      text NOT NULL,
    external_id text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION users_placeholder_claimed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM user_placeholders WHERE user_id = NEW.id;
    RETURN NULL;
END $$;
-- +goose StatementEnd
-- Activated, adopted by another provisioning source, or deleted: no longer a placeholder.
CREATE TRIGGER users_placeholder_claimed AFTER UPDATE ON users
    FOR EACH ROW WHEN ((NEW.active AND NOT OLD.active) OR NEW.source IS DISTINCT FROM OLD.source
                       OR (NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL))
    EXECUTE FUNCTION users_placeholder_claimed();

-- +goose Down
DROP TRIGGER users_placeholder_claimed ON users;
DROP FUNCTION users_placeholder_claimed();
DROP TABLE user_placeholders, jira_import_map, jira_imports;
