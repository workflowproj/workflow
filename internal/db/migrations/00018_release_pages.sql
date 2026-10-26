-- +goose Up
-- Releases: a page per release with readiness, release notes (shown in service desks' help
-- centres when published), the Git tag a release ships as, and deployments from GitHub and
-- GitLab with the issues each one contains.

-- Release notes are Markdown kept on the release. Published notes appear on the What's new page
-- of the help centres that chose the release's project.
ALTER TABLE releases ADD COLUMN notes text NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN notes_published_at timestamptz;
-- The Git tag the release ships as: linked by hand, or when a tag named like the release (with
-- or without a leading "v") reaches a repository connected to the project. The sha is known when
-- the provider sends it (GitLab tag pushes, GitHub pushes of tags).
ALTER TABLE releases ADD COLUMN tag_repository_id uuid REFERENCES scm_repositories (id) ON DELETE SET NULL;
ALTER TABLE releases ADD COLUMN tag_name text;
ALTER TABLE releases ADD COLUMN tag_sha text;
ALTER TABLE releases ADD COLUMN tag_linked_at timestamptz;

-- What a new tag does in a project (both off unless a project admin turns them on).
CREATE TABLE project_release_settings (
    project_id       uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    -- A tag without a release of its name creates one.
    create_from_tags boolean NOT NULL DEFAULT false,
    -- A release whose tag is pushed is marked released.
    release_on_tag   boolean NOT NULL DEFAULT false,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- The software projects whose published release notes a service desk's help centre shows.
CREATE TABLE service_desk_release_projects (
    desk_project_id uuid NOT NULL REFERENCES service_desks (project_id) ON DELETE CASCADE,
    project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    added_by        uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (desk_project_id, project_id)
);
CREATE INDEX service_desk_release_projects_project_idx ON service_desk_release_projects (project_id);

-- Deployments reported by GitHub (deployment_status) and GitLab (deployment events).
CREATE TABLE deployments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id   uuid NOT NULL REFERENCES scm_repositories (id) ON DELETE CASCADE,
    -- The provider's deployment id.
    external_id     text NOT NULL,
    environment     text NOT NULL,
    ref             text NOT NULL DEFAULT '',
    sha             text NOT NULL DEFAULT '',
    status          text NOT NULL CHECK (status IN ('pending', 'running', 'success', 'failed', 'canceled')),
    -- The deployment's page (the job or workflow run), else the environment's URL.
    url             text NOT NULL DEFAULT '',
    deployer        text NOT NULL DEFAULT '',
    -- When the provider reported the current status; older reports arriving late are ignored.
    status_at       timestamptz NOT NULL,
    -- Set once the issues it contains are known: the sha of the previous successful deployment
    -- to the environment it was compared with (NULL for the first), the commits read and
    -- whether the comparison was capped.
    resolved_at     timestamptz,
    base_sha        text,
    commit_count    integer NOT NULL DEFAULT 0,
    truncated       boolean NOT NULL DEFAULT false,
    -- The completed status deployment.completed was raised for, so a re-run that changes the
    -- outcome raises it again and a redelivery does not.
    notified_status text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (repository_id, external_id)
);
CREATE INDEX deployments_environment_idx ON deployments (repository_id, lower(environment), status_at DESC) WHERE status = 'success';
CREATE INDEX deployments_ref_idx ON deployments (repository_id, ref) WHERE status = 'success';

-- The issues a deployment contains: those its new commits mention or are linked to.
CREATE TABLE deployment_issues (
    deployment_id uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    issue_id      uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    PRIMARY KEY (deployment_id, issue_id)
);
CREATE INDEX deployment_issues_issue_idx ON deployment_issues (issue_id);

-- +goose Down
DROP TABLE deployment_issues, deployments, service_desk_release_projects, project_release_settings;
ALTER TABLE releases DROP COLUMN tag_linked_at, DROP COLUMN tag_sha, DROP COLUMN tag_name,
    DROP COLUMN tag_repository_id, DROP COLUMN notes_published_at, DROP COLUMN notes;
