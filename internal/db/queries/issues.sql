-- name: GetIssue :one
SELECT * FROM issues WHERE id = $1;

-- name: GetIssueForUpdate :one
SELECT * FROM issues WHERE id = $1 FOR UPDATE;

-- name: GetIssueByNumber :one
SELECT * FROM issues WHERE project_id = $1 AND number = $2;

-- name: GetIssuesByIDs :many
SELECT * FROM issues WHERE id = ANY(@ids::uuid[]);

-- name: CreateIssue :one
INSERT INTO issues (project_id, number, type_id, parent_id, summary, description, status_id, workflow_version_id,
                    priority, assignee_id, reporter_id, sprint_id, story_points, start_date, due_date, labels,
                    resolution, resolved_at, fields, rank, original_estimate_seconds, remaining_estimate_seconds)
VALUES (@project_id, @number, @type_id, @parent_id, @summary, @description, @status_id, @workflow_version_id,
        @priority, @assignee_id, @reporter_id, @sprint_id, @story_points, @start_date, @due_date, @labels,
        @resolution, @resolved_at, @fields, @rank, @original_estimate_seconds, @remaining_estimate_seconds)
RETURNING *;

-- name: UpdateIssue :one
-- Full-row write guarded by the optimistic concurrency token.
UPDATE issues SET
    type_id = @type_id, parent_id = @parent_id, summary = @summary, description = @description,
    status_id = @status_id, workflow_version_id = @workflow_version_id, priority = @priority,
    assignee_id = @assignee_id, reporter_id = @reporter_id, sprint_id = @sprint_id, story_points = @story_points,
    start_date = @start_date, due_date = @due_date, labels = @labels, resolution = @resolution,
    resolved_at = @resolved_at, fields = @fields, rank = @rank,
    original_estimate_seconds = @original_estimate_seconds, remaining_estimate_seconds = @remaining_estimate_seconds,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING *;

-- name: DeleteIssue :exec
DELETE FROM issues WHERE id = $1;

-- name: DeleteSubtasks :exec
DELETE FROM issues WHERE parent_id = $1 AND type_id IN (SELECT id FROM issue_types WHERE level < 0);

-- name: ChildIssues :many
SELECT * FROM issues WHERE parent_id = $1 ORDER BY rank;

-- name: CountChildren :one
SELECT count(*) FROM issues WHERE parent_id = $1;

-- name: SetChildrenSprint :exec
-- Sub-tasks always follow their parent's sprint.
UPDATE issues SET sprint_id = @sprint_id, version = version + 1, updated_at = now()
WHERE parent_id = @parent_id AND type_id IN (SELECT id FROM issue_types WHERE level < 0);

-- name: SetIssueRank :one
UPDATE issues SET rank = @rank, version = version + 1, updated_at = now() WHERE id = @id RETURNING *;

-- name: MaxRank :one
SELECT coalesce(max(rank), '')::text FROM issues WHERE project_id = $1;

-- name: NextRanksAfter :many
SELECT id, rank FROM issues WHERE project_id = @project_id AND rank > @rank::text ORDER BY rank LIMIT 2;

-- name: PrevRanksBefore :many
SELECT id, rank FROM issues WHERE project_id = @project_id AND rank < @rank::text ORDER BY rank DESC LIMIT 2;

-- name: EpicProgress :many
SELECT i.parent_id, s.category, count(*)::int AS n, coalesce(sum(i.story_points), 0)::float8 AS points
FROM issues i JOIN statuses s ON s.id = i.status_id
WHERE i.parent_id = ANY(@ids::uuid[])
GROUP BY i.parent_id, s.category;

-- ---------------------------------------------------------------------------
-- History

-- name: InsertHistory :exec
INSERT INTO issue_history (issue_id, actor_id, actor_type, field, from_value, to_value, from_ref, to_ref, via)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg(via));

-- name: ListHistory :many
-- By time, then insertion order (imported history carries the original timestamps).
SELECT * FROM issue_history WHERE issue_id = $1 ORDER BY created_at, id;

-- ---------------------------------------------------------------------------
-- Links

-- name: CreateIssueLink :one
INSERT INTO issue_links (source_id, target_id, link_type, created_by) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetIssueLink :one
SELECT * FROM issue_links WHERE id = $1;

-- name: DeleteIssueLink :exec
DELETE FROM issue_links WHERE id = $1;

-- name: IssueLinks :many
SELECT * FROM issue_links WHERE source_id = $1 OR target_id = $1 ORDER BY created_at;

-- name: LinksOfType :many
-- Links of one type that touch any of the issues, oldest first.
SELECT * FROM issue_links
WHERE link_type = @link_type AND (source_id = ANY(@ids::uuid[]) OR target_id = ANY(@ids::uuid[]))
ORDER BY created_at;

-- name: LinkedWorkProgress :many
-- For each target, how many issues link to it with a link type and how many of those are done
-- (mitigating work of a risk, implementing work of a decision).
SELECT l.target_id, count(*)::int AS total, count(*) FILTER (WHERE s.category = 'done')::int AS done
FROM issue_links l
JOIN issues o ON o.id = l.source_id
JOIN statuses s ON s.id = o.status_id
WHERE l.target_id = ANY(@ids::uuid[]) AND l.link_type = @link_type
GROUP BY l.target_id;

-- name: ProjectIssuesOfKind :many
-- A project's issues of one issue-type kind (risks, decisions), newest first.
SELECT i.* FROM issues i JOIN issue_types t ON t.id = i.type_id
WHERE i.project_id = @project_id AND t.kind = @kind
ORDER BY i.number DESC
LIMIT @lim;

-- ---------------------------------------------------------------------------
-- Comments

-- name: CreateComment :one
INSERT INTO comments (issue_id, author_id, body, source, via, internal) VALUES ($1, $2, $3, $4, sqlc.narg(via), @internal) RETURNING *;

-- name: GetComment :one
SELECT * FROM comments WHERE id = $1;

-- name: UpdateComment :one
UPDATE comments SET body = $2, edited_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteComment :exec
DELETE FROM comments WHERE id = $1;

-- name: ListComments :many
SELECT * FROM comments WHERE issue_id = $1 ORDER BY created_at;

-- ---------------------------------------------------------------------------
-- Watchers

-- name: AddWatcher :exec
INSERT INTO watchers (issue_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: RemoveWatcher :exec
DELETE FROM watchers WHERE issue_id = $1 AND user_id = $2;

-- name: ListWatchers :many
SELECT u.* FROM watchers w JOIN users u ON u.id = w.user_id WHERE w.issue_id = $1 ORDER BY u.display_name;

-- name: WatcherIDs :many
SELECT user_id FROM watchers WHERE issue_id = $1;

-- name: IsWatching :one
SELECT EXISTS (SELECT 1 FROM watchers WHERE issue_id = $1 AND user_id = $2);

-- ---------------------------------------------------------------------------
-- Attachments & blobs

-- name: CreateAttachment :one
INSERT INTO attachments (issue_id, uploader_id, filename, content_type, size_bytes, object_key, public)
VALUES ($1, $2, $3, $4, $5, $6, @public)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1;

-- name: ListAttachments :many
SELECT * FROM attachments WHERE issue_id = $1 ORDER BY created_at;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;

-- name: PutBlob :exec
INSERT INTO blobs (key, data) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET data = excluded.data;

-- name: GetBlob :one
SELECT data FROM blobs WHERE key = $1;

-- name: DeleteBlob :exec
DELETE FROM blobs WHERE key = $1;

-- ---------------------------------------------------------------------------
-- Releases on issues

-- name: IssueReleases :many
SELECT r.* FROM releases r JOIN issue_releases ir ON ir.release_id = r.id WHERE ir.issue_id = $1 ORDER BY r.position, r.name;

-- name: ClearIssueReleases :exec
DELETE FROM issue_releases WHERE issue_id = $1;

-- name: AddIssueRelease :exec
INSERT INTO issue_releases (issue_id, release_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Notifications & saved filters

-- name: CreateNotification :exec
INSERT INTO notifications (user_id, issue_id, kind, title, body, actor_id, email_delivery) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListNotifications :many
SELECT n.*, coalesce(p.key || '-' || i.number, '')::text AS issue_key
FROM notifications n
LEFT JOIN issues i ON i.id = n.issue_id
LEFT JOIN projects p ON p.id = i.project_id
WHERE n.user_id = @user_id AND (NOT @unread_only::bool OR n.read_at IS NULL)
ORDER BY n.id DESC
LIMIT @lim;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationsRead :exec
UPDATE notifications SET read_at = now()
WHERE user_id = @user_id AND read_at IS NULL AND (cardinality(@ids::bigint[]) = 0 OR id = ANY(@ids::bigint[]));

-- name: ListFilters :many
SELECT * FROM saved_filters WHERE owner_id = $1 OR shared ORDER BY name;

-- name: GetFilter :one
SELECT * FROM saved_filters WHERE id = $1;

-- name: CreateFilter :one
INSERT INTO saved_filters (owner_id, name, description, jql, shared) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: UpdateFilter :one
UPDATE saved_filters SET name = $2, description = $3, jql = $4, shared = $5, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteFilter :exec
DELETE FROM saved_filters WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Reports

-- name: BlockLinksAmong :many
SELECT * FROM issue_links
WHERE link_type = 'blocks' AND source_id = ANY(@ids::uuid[]) AND target_id = ANY(@ids::uuid[])
ORDER BY created_at;
