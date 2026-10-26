-- Release pages: progress, readiness, notes, tags and the help centre's What's new.

-- name: ReleaseIssueIDs :many
SELECT issue_id FROM issue_releases WHERE release_id = $1;

-- name: FixVersionHistory :many
-- Fix version changes in a project that name a release, by id (recorded refs) or by name (older
-- history and imports), oldest first: the burnup replays them for scope.
SELECT h.issue_id, h.from_value, h.to_value, h.from_ref, h.to_ref, h.created_at
FROM issue_history h JOIN issues i ON i.id = h.issue_id
WHERE i.project_id = @project_id AND h.field = 'fixVersions'
  AND (position(@release_id::text IN coalesce(h.from_ref, '') || ',' || coalesce(h.to_ref, '')) > 0
       OR position(lower(@name::text) IN lower(coalesce(h.from_value, '') || ', ' || coalesce(h.to_value, ''))) > 0)
ORDER BY h.issue_id, h.created_at, h.id;

-- name: OpenPullRequestsOf :many
-- Open and draft pull/merge requests linked to the given issues.
SELECT d.issue_id, d.external_id, d.title, d.url, d.state, r.full_path
FROM dev_links d JOIN scm_repositories r ON r.id = d.repository_id
WHERE d.issue_id = ANY(@ids::uuid[]) AND d.kind = 'pull_request' AND d.state IN ('open', 'draft')
ORDER BY d.issue_id, d.updated_at DESC;

-- name: LatestPipelinesOf :many
-- The most recent pipeline of each of the given issues.
SELECT DISTINCT ON (d.issue_id) d.issue_id, d.external_id, d.title, d.url, d.state, d.updated_at
FROM dev_links d
WHERE d.issue_id = ANY(@ids::uuid[]) AND d.kind = 'pipeline'
ORDER BY d.issue_id, d.updated_at DESC;

-- name: SetReleaseNotes :one
UPDATE releases SET notes = @notes,
    notes_published_at = CASE WHEN @published::boolean THEN coalesce(notes_published_at, now()) END
WHERE id = @id RETURNING *;

-- name: SetReleaseTag :one
UPDATE releases SET tag_repository_id = sqlc.narg(repository_id), tag_name = sqlc.narg(tag_name),
    tag_sha = sqlc.narg(tag_sha), tag_linked_at = CASE WHEN sqlc.narg(tag_name)::text IS NULL THEN NULL ELSE now() END
WHERE id = @id RETURNING *;

-- name: SetReleaseTagSHA :exec
UPDATE releases SET tag_sha = @tag_sha WHERE id = @id;

-- name: ReleaseForTag :one
-- The project's release a tag names: the same name, or the name with or without a leading "v"
-- (v3.9.0 and 3.9.0), an exact match first.
SELECT * FROM releases
WHERE project_id = @project_id
  AND (lower(name) = lower(@tag::text) OR 'v' || lower(name) = lower(@tag::text) OR lower(name) = 'v' || lower(@tag::text))
ORDER BY lower(name) = lower(@tag::text) DESC, position
LIMIT 1;

-- name: PreviousTaggedRelease :one
-- The release before this one (in the releases list's order) with a tag in the same repository.
SELECT * FROM releases
WHERE project_id = @project_id AND tag_repository_id = @repository_id AND tag_name IS NOT NULL
  AND id <> @id AND position < @position
ORDER BY position DESC
LIMIT 1;

-- name: GetReleaseSettings :one
SELECT * FROM project_release_settings WHERE project_id = $1;

-- name: UpsertReleaseSettings :one
INSERT INTO project_release_settings (project_id, create_from_tags, release_on_tag)
VALUES (@project_id, @create_from_tags, @release_on_tag)
ON CONFLICT (project_id) DO UPDATE SET create_from_tags = excluded.create_from_tags,
    release_on_tag = excluded.release_on_tag, updated_at = now()
RETURNING *;

-- ---------------------------------------------------------------------------
-- The help centre's What's new

-- name: DeskReleaseProjects :many
SELECT p.* FROM service_desk_release_projects s JOIN projects p ON p.id = s.project_id
WHERE s.desk_project_id = $1
ORDER BY lower(p.name);

-- name: ClearDeskReleaseProjects :exec
DELETE FROM service_desk_release_projects WHERE desk_project_id = $1;

-- name: AddDeskReleaseProject :exec
INSERT INTO service_desk_release_projects (desk_project_id, project_id, added_by) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: PublishedReleaseNotes :many
-- Published notes of the projects a desk chose, newest first. Only what the help centre shows:
-- the product's name, the release's name, dates and notes.
SELECT r.id, r.name, r.notes, r.release_date, r.released_at, r.notes_published_at, p.name AS project_name
FROM releases r
JOIN service_desk_release_projects s ON s.project_id = r.project_id
JOIN projects p ON p.id = r.project_id
WHERE s.desk_project_id = @desk_project_id AND r.notes_published_at IS NOT NULL AND p.archived_at IS NULL
ORDER BY coalesce(r.released_at, r.notes_published_at) DESC, r.position DESC
LIMIT @lim;

-- name: CountPublishedReleaseNotes :one
SELECT count(*)::int FROM releases r JOIN service_desk_release_projects s ON s.project_id = r.project_id
JOIN projects p ON p.id = r.project_id
WHERE s.desk_project_id = $1 AND r.notes_published_at IS NOT NULL AND p.archived_at IS NULL;
