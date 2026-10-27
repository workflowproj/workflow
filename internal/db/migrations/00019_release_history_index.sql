-- +goose NO TRANSACTION
-- +goose Up
-- A release's burnup replays fix version changes from issue history, including issues that have
-- since left the release. issue_history is large, so the index is built without locking writes.
-- A build that failed part-way leaves an INVALID index behind and goose runs this again, so drop
-- any leftover first rather than skipping it with IF NOT EXISTS.
DROP INDEX CONCURRENTLY IF EXISTS issue_history_fix_versions_idx;
CREATE INDEX CONCURRENTLY issue_history_fix_versions_idx ON issue_history (issue_id) WHERE field = 'fixVersions';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS issue_history_fix_versions_idx;
