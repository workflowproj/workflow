-- +goose Up
-- Full-text search also matches English word forms ("backups" finds "backup", "upgrading" finds
-- "Upgrade"). A separate document rather than extra lexemes in issue_search_document: positions in
-- one concatenated vector would let phrases match across the join.
-- +goose StatementBegin
CREATE FUNCTION issue_search_document_english(summary text, description text) RETURNS tsvector
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT setweight(to_tsvector('english', coalesce(summary, '')), 'A') ||
           setweight(to_tsvector('english', coalesce(description, '')), 'B')
$$;
-- +goose StatementEnd
CREATE INDEX issues_search_english_idx ON issues USING gin (issue_search_document_english(summary, description));

-- +goose Down
DROP INDEX issues_search_english_idx;
DROP FUNCTION issue_search_document_english(text, text);
