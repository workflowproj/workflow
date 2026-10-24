-- name: ListAutomationRules :many
SELECT * FROM automation_rules WHERE project_id = $1 ORDER BY created_at;

-- name: GetAutomationRule :one
SELECT * FROM automation_rules WHERE id = $1;

-- name: EnabledRulesForTrigger :many
SELECT * FROM automation_rules WHERE project_id = $1 AND trigger_type = $2 AND enabled ORDER BY created_at;

-- name: CreateAutomationRule :one
INSERT INTO automation_rules (project_id, name, description, enabled, trigger_type, trigger_config, condition, actions, created_by, next_run_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpdateAutomationRule :one
UPDATE automation_rules SET name = $2, description = $3, enabled = $4, trigger_type = $5, trigger_config = $6,
    condition = $7, actions = $8, next_run_at = $9, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: DeleteAutomationRule :exec
DELETE FROM automation_rules WHERE id = $1;

-- name: DueScheduledRules :many
SELECT * FROM automation_rules
WHERE enabled AND trigger_type = 'schedule' AND next_run_at IS NOT NULL AND next_run_at <= now()
ORDER BY next_run_at
LIMIT 50
FOR UPDATE SKIP LOCKED;

-- name: MarkRuleRun :exec
UPDATE automation_rules SET last_run_at = now(), next_run_at = $2 WHERE id = $1;

-- name: InsertAutomationRun :exec
INSERT INTO automation_runs (rule_id, issue_id, status, message, depth) VALUES ($1, $2, $3, $4, $5);

-- name: ListAutomationRuns :many
SELECT ar.*, coalesce(p.key || '-' || i.number, '')::text AS issue_key
FROM automation_runs ar
LEFT JOIN issues i ON i.id = ar.issue_id
LEFT JOIN projects p ON p.id = i.project_id
WHERE ar.rule_id = $1 ORDER BY ar.id DESC LIMIT $2;

-- ---------------------------------------------------------------------------
-- Outbound webhooks

-- name: ListWebhookSubscriptions :many
SELECT * FROM webhook_subscriptions
WHERE (sqlc.narg(project_id)::uuid IS NULL AND project_id IS NULL) OR project_id = sqlc.narg(project_id)
ORDER BY created_at;

-- name: GetWebhookSubscription :one
SELECT * FROM webhook_subscriptions WHERE id = $1;

-- name: MatchingSubscriptions :many
SELECT * FROM webhook_subscriptions
WHERE active AND (project_id IS NULL OR project_id = sqlc.narg(project_id))
  AND (@event::text = ANY(events) OR '*' = ANY(events));

-- name: CreateWebhookSubscription :one
INSERT INTO webhook_subscriptions (project_id, name, url, secret_enc, events, active, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateWebhookSubscription :one
UPDATE webhook_subscriptions SET name = $2, url = $3, events = $4, active = $5, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteWebhookSubscription :exec
DELETE FROM webhook_subscriptions WHERE id = $1;

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (subscription_id, event, payload) VALUES ($1, $2, $3) RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE id = $1;

-- name: FinishWebhookDelivery :exec
UPDATE webhook_deliveries SET status = $2, attempts = attempts + 1, response_status = $3, last_error = $4,
    delivered_at = CASE WHEN $2 = 'delivered' THEN now() ELSE delivered_at END
WHERE id = $1;

-- name: ListWebhookDeliveries :many
SELECT * FROM webhook_deliveries WHERE subscription_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: TouchRuleLastRun :exec
UPDATE automation_rules SET last_run_at = now() WHERE id = $1;

-- name: DisableRulesUsingChannel :many
UPDATE automation_rules SET enabled = false, updated_at = now()
WHERE project_id = @project_id AND enabled
  AND actions @> jsonb_build_array(jsonb_build_object('channelId', @channel_id::text))
RETURNING id, name;
