-- +goose Up
-- Saved timelines: a named group of projects to plan together on one timeline (the sidebar's
-- Timeline opens on one of them instead of on every project). Private to their owner unless
-- shared. Sharing grants no access: each person sees only the projects they can browse.
CREATE TABLE timeline_views (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    shared      boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX timeline_views_owner_idx ON timeline_views (owner_id);

-- The projects of a saved timeline, in the order chosen. By id, so a renamed key keeps its place
-- and a deleted project drops out.
CREATE TABLE timeline_view_projects (
    view_id    uuid NOT NULL REFERENCES timeline_views (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    position   integer NOT NULL,
    PRIMARY KEY (view_id, project_id)
);
CREATE INDEX timeline_view_projects_project_idx ON timeline_view_projects (project_id);

-- +goose Down
DROP TABLE timeline_view_projects, timeline_views;
