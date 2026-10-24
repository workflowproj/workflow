-- +goose Up
-- Time tracking: estimates on issues and the work logged against them, all in seconds. Time
-- spent is the sum of an issue's worklogs; it is not stored on the issue.
ALTER TABLE issues
    ADD COLUMN original_estimate_seconds  bigint CHECK (original_estimate_seconds IS NULL OR original_estimate_seconds >= 0),
    ADD COLUMN remaining_estimate_seconds bigint CHECK (remaining_estimate_seconds IS NULL OR remaining_estimate_seconds >= 0);

CREATE TABLE worklogs (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id         uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    author_id        uuid REFERENCES users (id) ON DELETE SET NULL,
    -- When the work was done (the timesheet day), not when it was logged.
    started_at       timestamptz NOT NULL,
    duration_seconds bigint NOT NULL CHECK (duration_seconds > 0),
    comment          text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX worklogs_issue_idx ON worklogs (issue_id, started_at);
CREATE INDEX worklogs_author_idx ON worklogs (author_id, started_at);
CREATE INDEX worklogs_started_idx ON worklogs (started_at);

-- +goose Down
DROP TABLE worklogs;
ALTER TABLE issues DROP COLUMN remaining_estimate_seconds, DROP COLUMN original_estimate_seconds;
