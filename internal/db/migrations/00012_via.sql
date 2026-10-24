-- +goose Up
-- What a person used to make a change on their behalf, e.g. an AI assistant over MCP
-- ("Ada via Claude"); NULL for the web app and plain API calls.
ALTER TABLE issue_history ADD COLUMN via text;
ALTER TABLE comments ADD COLUMN via text;

-- +goose Down
ALTER TABLE comments DROP COLUMN via;
ALTER TABLE issue_history DROP COLUMN via;
