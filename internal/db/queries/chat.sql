-- name: CreateChatChannel :one
INSERT INTO chat_channels (project_id, provider, name, webhook_url_enc, url_hint, events, jql, active, created_by)
VALUES (@project_id, @provider, @name, @webhook_url_enc, @url_hint, @events, @jql, @active, @created_by)
RETURNING *;

-- name: GetChatChannel :one
SELECT * FROM chat_channels WHERE id = $1;

-- name: ListChatChannels :many
SELECT * FROM chat_channels WHERE project_id = $1 ORDER BY name, created_at;

-- name: UpdateChatChannel :one
UPDATE chat_channels SET name = @name, webhook_url_enc = @webhook_url_enc, url_hint = @url_hint, events = @events,
    jql = @jql, active = @active, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteChatChannel :exec
DELETE FROM chat_channels WHERE id = $1;

-- name: ChatChannelsForEvent :many
SELECT * FROM chat_channels
WHERE project_id = @project_id AND active AND (@event::text = ANY(events) OR '*' = ANY(events));

-- name: CreateChatDelivery :one
INSERT INTO chat_deliveries (channel_id, event, issue_key, payload)
VALUES (@channel_id, @event, @issue_key, @payload)
RETURNING *;

-- name: GetChatDelivery :one
SELECT * FROM chat_deliveries WHERE id = $1;

-- name: FinishChatDelivery :exec
UPDATE chat_deliveries SET status = @status, attempts = attempts + 1, response_status = @response_status,
    last_error = @last_error, delivered_at = CASE WHEN @status = 'delivered' THEN now() ELSE delivered_at END
WHERE id = @id;

-- name: ListChatDeliveries :many
SELECT * FROM chat_deliveries WHERE channel_id = @channel_id ORDER BY created_at DESC LIMIT @lim;

-- name: PruneChatDeliveries :execrows
DELETE FROM chat_deliveries WHERE created_at < @before;
