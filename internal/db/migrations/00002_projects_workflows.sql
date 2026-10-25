-- +goose Up
CREATE TABLE projects (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key           text NOT NULL UNIQUE CHECK (key ~ '^[A-Z][A-Z0-9]{1,9}$'),
    name          text NOT NULL,
    description   text NOT NULL DEFAULT '',
    lead_id       uuid REFERENCES users (id) ON DELETE SET NULL,
    template      text NOT NULL,
    -- Role every organisation member (not guests) gets implicitly. NULL = private project.
    open_role_id  uuid REFERENCES roles (id) ON DELETE SET NULL,
    -- Per-project issue counter, locked with SELECT ... FOR UPDATE when an issue is created.
    issue_counter integer NOT NULL DEFAULT 0,
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Old keys after a rename, so PLAT-12 in an old commit still resolves.
CREATE TABLE project_key_aliases (
    key        text PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE project_members (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid REFERENCES users (id) ON DELETE CASCADE,
    group_id   uuid REFERENCES groups (id) ON DELETE CASCADE,
    role_id    uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((user_id IS NULL) <> (group_id IS NULL))
);
CREATE UNIQUE INDEX project_members_user_key ON project_members (project_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX project_members_group_key ON project_members (project_id, group_id) WHERE group_id IS NOT NULL;
CREATE INDEX project_members_user_idx ON project_members (user_id);
CREATE INDEX project_members_group_idx ON project_members (group_id);

-- Statuses are global and identified by name, so boards and cross-project queries work across workflows.
CREATE TABLE statuses (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    category   text NOT NULL CHECK (category IN ('todo', 'in_progress', 'done')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX statuses_name_key ON statuses (lower(name));

-- A workflow is a versioned state machine. project_id NULL = shared across projects.
CREATE TABLE workflows (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid REFERENCES projects (id) ON DELETE CASCADE,
    name            text NOT NULL,
    description     text NOT NULL DEFAULT '',
    current_version integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX workflows_name_key ON workflows (coalesce(project_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));

CREATE TABLE workflow_versions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id  uuid NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    version      integer NOT NULL,
    -- Compiled definition (statuses resolved to ids); source is the YAML as authored.
    definition   jsonb NOT NULL,
    source_yaml  text NOT NULL,
    published_by uuid REFERENCES users (id) ON DELETE SET NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, version)
);

-- Issue types are copied into each project from its template. level: epic 1, standard 0, sub-task -1.
CREATE TABLE issue_types (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    key         text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,30}$'),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    icon        text NOT NULL DEFAULT 'task',
    color       text NOT NULL DEFAULT '#4f7cff',
    level       integer NOT NULL CHECK (level BETWEEN -1 AND 2),
    position    integer NOT NULL DEFAULT 0,
    -- Type -> workflow mapping.
    workflow_id uuid NOT NULL REFERENCES workflows (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, key)
);

-- Custom field definitions; values live in issues.fields (JSONB).
CREATE TABLE field_definitions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key         text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_]{0,40}$'),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    type        text NOT NULL CHECK (type IN ('text', 'number', 'select', 'multiselect', 'user', 'date', 'version', 'checkbox', 'url')),
    options     jsonb NOT NULL DEFAULT '[]',
    -- NULL = applies to every project / issue type.
    project_ids uuid[],
    issue_type_keys text[],
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE field_definitions, issue_types, workflow_versions, workflows, statuses, project_members, project_key_aliases, projects CASCADE;
