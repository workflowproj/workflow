-- +goose Up
-- What board cards show besides the summary, type and key, in order.
ALTER TABLE boards ADD COLUMN card_fields text[] NOT NULL DEFAULT '{epic,labels,priority,dev,points,assignee}';

-- When an issue entered its current status ("days in column", time-in-status metrics). Kept by
-- a trigger so every path that changes the status (transitions, moves, workflow migrations)
-- updates it.
ALTER TABLE issues ADD COLUMN status_changed_at timestamptz;
UPDATE issues i SET status_changed_at = coalesce(
    (SELECT max(h.created_at) FROM issue_history h WHERE h.issue_id = i.id AND h.field = 'status'), i.created_at);
ALTER TABLE issues ALTER COLUMN status_changed_at SET NOT NULL, ALTER COLUMN status_changed_at SET DEFAULT now();

-- +goose StatementBegin
CREATE FUNCTION issues_status_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status_id IS DISTINCT FROM OLD.status_id THEN
        NEW.status_changed_at := now();
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER issues_status_changed BEFORE UPDATE OF status_id ON issues
    FOR EACH ROW EXECUTE FUNCTION issues_status_changed();

-- Metric history: the sprint field and created events are read in bulk by reports.
CREATE INDEX issue_history_sprint_idx ON issue_history (issue_id, created_at) WHERE field IN ('sprint', 'storyPoints');

-- Personal dashboards, optionally shared with the whole organisation. Gadgets are a JSON list
-- of {id, type, title, width, config}; each gadget's data is fetched with the viewer's access.
CREATE TABLE dashboards (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    shared      boolean NOT NULL DEFAULT false,
    gadgets     jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dashboards_owner_idx ON dashboards (owner_id);
CREATE INDEX dashboards_shared_idx ON dashboards (updated_at DESC) WHERE shared;

-- +goose Down
DROP TABLE dashboards;
DROP INDEX issue_history_sprint_idx;
DROP TRIGGER issues_status_changed ON issues;
DROP FUNCTION issues_status_changed();
ALTER TABLE issues DROP COLUMN status_changed_at;
ALTER TABLE boards DROP COLUMN card_fields;
