
-- name: EmailRecipient :one
-- A notification recipient and their email settings (NULL settings mean the defaults).
SELECT u.id, u.email, u.display_name, u.kind, u.active, u.deleted_at,
       s.deliveries, s.paused
FROM users u
LEFT JOIN email_notification_settings s ON s.user_id = u.id
WHERE u.id = $1;

-- name: GetEmailSettings :one
SELECT * FROM email_notification_settings WHERE user_id = $1;

-- name: UpsertEmailSettings :one
INSERT INTO email_notification_settings (user_id, deliveries, digest_hour, time_zone, paused, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (user_id) DO UPDATE SET deliveries = EXCLUDED.deliveries, digest_hour = EXCLUDED.digest_hour,
    time_zone = EXCLUDED.time_zone, paused = EXCLUDED.paused, updated_at = now()
RETURNING *;

-- name: RecordDigestSent :exec
INSERT INTO email_notification_settings (user_id, last_digest_at) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET last_digest_at = EXCLUDED.last_digest_at;

-- name: PendingEmailNotifications :many
-- Notifications waiting to be emailed to one person (on one issue, when issue_id is given),
-- locked so concurrent senders skip them.
SELECT n.id, n.kind, n.title, n.body, n.created_at, n.issue_id, n.actor_id,
       coalesce(a.display_name, '')::text AS actor_name
FROM notifications n
LEFT JOIN users a ON a.id = n.actor_id
WHERE n.user_id = @user_id AND n.email_delivery = @delivery::text AND n.emailed_at IS NULL
  AND (sqlc.narg(issue_id)::uuid IS NULL OR n.issue_id = sqlc.narg(issue_id))
ORDER BY n.id
LIMIT 2000
FOR UPDATE OF n SKIP LOCKED;

-- name: MarkNotificationsEmailed :exec
UPDATE notifications SET emailed_at = now() WHERE id = ANY(@ids::bigint[]);

-- name: EmailIssues :many
SELECT i.id, i.number, i.summary, i.assignee_id, i.reporter_id,
       p.id AS project_id, p.key AS project_key, p.name AS project_name,
       st.name AS status_name, st.category AS status_category
FROM issues i
JOIN projects p ON p.id = i.project_id
JOIN statuses st ON st.id = i.status_id
WHERE i.id = ANY(@ids::uuid[]);

-- name: WatchedAmong :many
SELECT issue_id FROM watchers WHERE user_id = @user_id AND issue_id = ANY(@issue_ids::uuid[]);

-- name: DigestCandidates :many
-- People with notifications waiting for their digest, the oldest one, and when they want it.
SELECT n.user_id, min(n.created_at)::timestamptz AS oldest, s.digest_hour, s.time_zone
FROM notifications n
LEFT JOIN email_notification_settings s ON s.user_id = n.user_id
WHERE n.email_delivery = 'digest' AND n.emailed_at IS NULL
GROUP BY n.user_id, s.digest_hour, s.time_zone
LIMIT 5000;

-- name: StaleImmediateEmails :many
-- Immediate emails whose job should have run by now (a safety net; normally none).
SELECT DISTINCT user_id, issue_id::uuid AS issue_id FROM notifications
WHERE email_delivery = 'immediate' AND emailed_at IS NULL AND issue_id IS NOT NULL AND created_at < @before
LIMIT 1000;
