-- +goose NO TRANSACTION
-- +goose Up
-- A release's burnup replays fix version changes from issue history, including issues that have
-- since left the release. issue_history is large, so the index is built without locking writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_history_fix_versions_idx ON issue_history (issue_id) WHERE field = 'fixVersions';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS issue_history_fix_versions_idx;
