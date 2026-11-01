-- +goose Up
-- Programmes: a project made from the programme template holds initiatives (level 2), and lists
-- the team projects that work toward them. An epic in a member project may have one of the
-- programme's initiatives as its parent; that is the only parent link that crosses projects.
-- Membership grants no access: everything shown is still limited to projects people can browse.
CREATE TABLE programme_projects (
    programme_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    added_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (programme_id, project_id),
    CHECK (programme_id <> project_id)
);
CREATE INDEX programme_projects_project_idx ON programme_projects (project_id);

-- +goose Down
DROP TABLE programme_projects;
