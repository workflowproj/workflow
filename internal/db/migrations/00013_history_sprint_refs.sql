-- +goose Up
-- Sprint reports and velocity find the issues moved in or out of a sprint from its history.
CREATE INDEX issue_history_sprint_to_idx ON issue_history (to_ref) WHERE field = 'sprint';
CREATE INDEX issue_history_sprint_from_idx ON issue_history (from_ref) WHERE field = 'sprint';

-- +goose Down
DROP INDEX issue_history_sprint_from_idx;
DROP INDEX issue_history_sprint_to_idx;
