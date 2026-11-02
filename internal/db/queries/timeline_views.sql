-- name: ListTimelineViews :many
-- A person's saved timelines and those shared with everyone, by name.
SELECT * FROM timeline_views WHERE owner_id = $1 OR shared ORDER BY lower(name), created_at;

-- name: GetTimelineView :one
SELECT * FROM timeline_views WHERE id = $1;

-- name: CreateTimelineView :one
INSERT INTO timeline_views (owner_id, name, description, shared) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateTimelineView :one
UPDATE timeline_views SET name = $2, description = $3, shared = $4, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteTimelineView :exec
DELETE FROM timeline_views WHERE id = $1;

-- name: TimelineViewProjects :many
-- The (unarchived) projects of the given saved timelines, in each one's chosen order.
SELECT tvp.view_id, sqlc.embed(p)
FROM timeline_view_projects tvp JOIN projects p ON p.id = tvp.project_id
WHERE tvp.view_id = ANY(@view_ids::uuid[]) AND p.archived_at IS NULL
ORDER BY tvp.view_id, tvp.position;

-- name: ClearTimelineViewProjects :exec
DELETE FROM timeline_view_projects WHERE view_id = $1;

-- name: AddTimelineViewProject :exec
INSERT INTO timeline_view_projects (view_id, project_id, position) VALUES ($1, $2, $3);
