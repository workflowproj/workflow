-- +goose NO TRANSACTION
-- +goose Up
-- Full-text search also matches English word forms ("backups" finds "backup", "upgrading" finds
-- "Upgrade"). A separate document rather than extra lexemes in issue_search_document: positions in
-- one concatenated vector would let phrases match across the join.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_search_document_english(summary text, description text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT setweight(to_tsvector('english', coalesce(summary, '')), 'A') ||
           setweight(to_tsvector('english', coalesce(description, '')), 'B')
$$;
-- +goose StatementEnd
-- issues is large and this runs while the previous version is serving, so the index is built
-- without blocking writes. A build that failed part-way leaves an INVALID index behind and goose
-- runs this again, so drop any leftover first.
DROP INDEX CONCURRENTLY IF EXISTS issues_search_english_idx;
CREATE INDEX CONCURRENTLY issues_search_english_idx ON issues USING gin (issue_search_document_english(summary, description));

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS issues_search_english_idx;
DROP FUNCTION IF EXISTS issue_search_document_english(text, text);
