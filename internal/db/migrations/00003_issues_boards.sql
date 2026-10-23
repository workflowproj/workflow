-- +goose Up
-- Full-text document for JQL `text ~` searches; backed by an expression index.
-- +goose StatementBegin
CREATE FUNCTION issue_search_document(summary text, description text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT setweight(to_tsvector('simple', coalesce(summary, '')), 'A') ||
           setweight(to_tsvector('simple', coalesce(description, '')), 'B')
$$;
-- +goose StatementEnd

CREATE TABLE sprints (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name         text NOT NULL,
    goal         text NOT NULL DEFAULT '',
    state        text NOT NULL DEFAULT 'future' CHECK (state IN ('future', 'active', 'closed')),
    start_date   date,
    end_date     date,
    completed_at timestamptz,
    position     integer NOT NULL DEFAULT 0,
    -- Snapshots for the sprint report: what was committed at start, what was done at completion.
    committed_issue_ids  uuid[] NOT NULL DEFAULT '{}',
    committed_points     numeric(10, 2) NOT NULL DEFAULT 0,
    completed_issue_ids  uuid[] NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX sprints_one_active_key ON sprints (project_id) WHERE state = 'active';
CREATE INDEX sprints_project_idx ON sprints (project_id, state);

CREATE TABLE releases (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    start_date   date,
    release_date date,
    state        text NOT NULL DEFAULT 'unreleased' CHECK (state IN ('unreleased', 'released', 'archived')),
    released_at  timestamptz,
    position     integer NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX releases_name_key ON releases (project_id, lower(name));

CREATE TABLE issues (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id          uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    number              integer NOT NULL,
    type_id             uuid NOT NULL REFERENCES issue_types (id),
    parent_id           uuid REFERENCES issues (id) ON DELETE SET NULL,
    summary             text NOT NULL,
    description         text NOT NULL DEFAULT '',
    status_id           uuid NOT NULL REFERENCES statuses (id),
    -- Issues pin to the workflow version they were created or migrated under.
    workflow_version_id uuid NOT NULL REFERENCES workflow_versions (id),
    priority            text NOT NULL DEFAULT 'medium' CHECK (priority IN ('highest', 'high', 'medium', 'low', 'lowest')),
    assignee_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    reporter_id         uuid REFERENCES users (id) ON DELETE SET NULL,
    sprint_id           uuid REFERENCES sprints (id) ON DELETE SET NULL,
    story_points        numeric(8, 2),
    start_date          date,
    due_date            date,
    labels              text[] NOT NULL DEFAULT '{}',
    resolution          text,
    resolved_at         timestamptz,
    -- Custom field values keyed by field_definitions.key.
    fields              jsonb NOT NULL DEFAULT '{}',
    -- Fractional (LexoRank-style) rank; byte ordering so it matches the Go implementation.
    rank                text COLLATE "C" NOT NULL,
    -- Optimistic concurrency token, bumped on every write.
    version             integer NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, number),
    UNIQUE (project_id, rank)
);
CREATE INDEX issues_parent_idx ON issues (parent_id);
CREATE INDEX issues_sprint_idx ON issues (sprint_id);
CREATE INDEX issues_status_idx ON issues (status_id);
CREATE INDEX issues_assignee_idx ON issues (assignee_id);
CREATE INDEX issues_type_idx ON issues (type_id);
CREATE INDEX issues_workflow_version_idx ON issues (workflow_version_id);
CREATE INDEX issues_updated_idx ON issues (project_id, updated_at DESC);
CREATE INDEX issues_labels_idx ON issues USING gin (labels);
CREATE INDEX issues_fields_idx ON issues USING gin (fields jsonb_path_ops);
CREATE INDEX issues_search_idx ON issues USING gin (issue_search_document(summary, description));
CREATE INDEX issues_summary_trgm ON issues USING gin (summary gin_trgm_ops);

CREATE TABLE issue_releases (
    issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    release_id uuid NOT NULL REFERENCES releases (id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, release_id)
);
CREATE INDEX issue_releases_release_idx ON issue_releases (release_id);

CREATE TABLE issue_links (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id  uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    target_id  uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    link_type  text NOT NULL CHECK (link_type IN ('blocks', 'relates', 'duplicates', 'clones')),
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, target_id, link_type),
    CHECK (source_id <> target_id)
);
CREATE INDEX issue_links_target_idx ON issue_links (target_id);

CREATE TABLE comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    author_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    body       text NOT NULL,
    -- user | scm (smart commit) | automation
    source     text NOT NULL DEFAULT 'user',
    edited_at  timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX comments_issue_idx ON comments (issue_id, created_at);

CREATE TABLE watchers (
    issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id)
);
CREATE INDEX watchers_user_idx ON watchers (user_id);

CREATE TABLE attachments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id     uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    uploader_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    filename     text NOT NULL,
    content_type text NOT NULL,
    size_bytes   bigint NOT NULL,
    -- S3 object key, or the blob id when using the Postgres storage backend.
    object_key   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attachments_issue_idx ON attachments (issue_id);

-- Fallback blob store so PostgreSQL stays the only required stateful dependency.
CREATE TABLE blobs (
    key        text PRIMARY KEY,
    data       bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Append-only changelog: drives the activity stream and cycle-time reports.
CREATE TABLE issue_history (
    id         bigserial PRIMARY KEY,
    issue_id   uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    -- user | automation | scm | system
    actor_type text NOT NULL DEFAULT 'user',
    field      text NOT NULL,
    from_value text,
    to_value   text,
    -- Machine-readable ids (status ids, user ids) alongside the display values.
    from_ref   text,
    to_ref     text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX issue_history_issue_idx ON issue_history (issue_id, id);
CREATE INDEX issue_history_status_idx ON issue_history (field, created_at) WHERE field = 'status';

CREATE TABLE boards (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name               text NOT NULL,
    type               text NOT NULL CHECK (type IN ('scrum', 'kanban')),
    -- Extra JQL applied on top of "project = KEY".
    filter_jql         text NOT NULL DEFAULT '',
    swimlanes          text NOT NULL DEFAULT 'none' CHECK (swimlanes IN ('none', 'assignee', 'epic', 'priority', 'type')),
    -- Kanban: hide done issues resolved longer ago than this.
    done_retention_days integer NOT NULL DEFAULT 14,
    position           integer NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX boards_project_idx ON boards (project_id);

CREATE TABLE board_columns (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id   uuid NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
    name       text NOT NULL,
    position   integer NOT NULL,
    status_ids uuid[] NOT NULL DEFAULT '{}',
    wip_limit  integer CHECK (wip_limit IS NULL OR wip_limit > 0)
);
CREATE INDEX board_columns_board_idx ON board_columns (board_id, position);

CREATE TABLE saved_filters (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    jql         text NOT NULL,
    shared      boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX saved_filters_owner_idx ON saved_filters (owner_id);

CREATE TABLE notifications (
    id         bigserial PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    issue_id   uuid REFERENCES issues (id) ON DELETE CASCADE,
    kind       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    actor_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, id DESC);

-- +goose Down
DROP TABLE notifications, saved_filters, board_columns, boards, issue_history, blobs, attachments, watchers, comments, issue_links, issue_releases, issues, releases, sprints CASCADE;
DROP FUNCTION issue_search_document(text, text);
