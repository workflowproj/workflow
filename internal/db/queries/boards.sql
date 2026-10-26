-- name: ListBoards :many
SELECT * FROM boards WHERE project_id = $1 ORDER BY position, created_at;

-- name: GetBoard :one
SELECT * FROM boards WHERE id = $1;

-- name: CreateBoard :one
INSERT INTO boards (project_id, name, type, filter_jql, swimlanes, done_retention_days, position)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateBoard :one
UPDATE boards SET name = $2, type = $3, filter_jql = $4, swimlanes = $5, done_retention_days = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteBoard :exec
DELETE FROM boards WHERE id = $1;

-- name: BoardColumns :many
SELECT * FROM board_columns WHERE board_id = $1 ORDER BY position;

-- name: DeleteBoardColumns :exec
DELETE FROM board_columns WHERE board_id = $1;

-- name: CreateBoardColumn :one
INSERT INTO board_columns (board_id, name, position, status_ids, wip_limit) VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- ---------------------------------------------------------------------------
-- Sprints

-- name: ListSprints :many
SELECT * FROM sprints
WHERE project_id = @project_id AND (@state::text = '' OR state = @state)
ORDER BY CASE state WHEN 'active' THEN 0 WHEN 'future' THEN 1 ELSE 2 END, position, created_at;

-- name: GetSprint :one
SELECT * FROM sprints WHERE id = $1;

-- name: GetSprintsByIDs :many
SELECT * FROM sprints WHERE id = ANY(@ids::uuid[]);

-- name: ActiveSprint :one
SELECT * FROM sprints WHERE project_id = $1 AND state = 'active';

-- name: CreateSprint :one
INSERT INTO sprints (project_id, name, goal, start_date, end_date, position)
VALUES (@project_id, @name, @goal, @start_date, @end_date,
        (SELECT coalesce(max(position), 0) + 1 FROM sprints WHERE project_id = @project_id))
RETURNING *;

-- name: UpdateSprint :one
UPDATE sprints SET name = $2, goal = $3, start_date = $4, end_date = $5 WHERE id = $1 RETURNING *;

-- name: StartSprint :one
UPDATE sprints SET state = 'active', start_date = @start_date, end_date = @end_date, goal = @goal,
    committed_issue_ids = coalesce((SELECT array_agg(ci.id) FROM issues ci WHERE ci.sprint_id = @id::uuid), '{}'),
    committed_points = coalesce((SELECT sum(ci.story_points) FROM issues ci WHERE ci.sprint_id = @id::uuid), 0)
WHERE sprints.id = @id::uuid AND state = 'future' RETURNING *;

-- name: CompleteSprint :one
UPDATE sprints SET state = 'closed', completed_at = now(),
    completed_issue_ids = coalesce((SELECT array_agg(i.id) FROM issues i JOIN statuses s ON s.id = i.status_id
                                    WHERE i.sprint_id = @id::uuid AND s.category = 'done'), '{}')
WHERE sprints.id = @id::uuid AND state = 'active' RETURNING *;

-- name: SprintDoneEvents :many
-- When each issue first entered a done status (for burndown charts).
SELECT h.issue_id, min(h.created_at)::timestamptz AS done_at
FROM issue_history h JOIN statuses s ON s.id::text = h.to_ref
WHERE h.issue_id = ANY(@ids::uuid[]) AND h.field = 'status' AND s.category = 'done'
GROUP BY h.issue_id;

-- name: DeleteSprint :exec
DELETE FROM sprints WHERE id = $1;

-- name: MoveOpenSprintIssues :one
-- Unfinished issues (not in a done status) leave a completed sprint. Sub-tasks go with their
-- parent whatever their own status, since they always follow its sprint. Returns how many
-- issues moved, not counting sub-tasks.
WITH open_issues AS (
    SELECT i.id FROM issues i
    JOIN issue_types t ON t.id = i.type_id
    JOIN statuses s ON s.id = i.status_id
    WHERE i.sprint_id = @sprint_id AND t.level >= 0 AND s.category <> 'done'
), moved AS (
    UPDATE issues SET sprint_id = sqlc.narg(target_sprint_id), version = version + 1, updated_at = now()
    WHERE issues.id IN (SELECT id FROM open_issues)
       OR (issues.sprint_id = @sprint_id AND issues.parent_id IN (SELECT id FROM open_issues)
           AND issues.type_id IN (SELECT id FROM issue_types WHERE level < 0))
    RETURNING issues.id
)
SELECT count(*)::int FROM moved WHERE moved.id IN (SELECT id FROM open_issues);

-- name: SprintIssueStats :one
-- A sprint's issues and story points now; sub-tasks count as part of their parent.
SELECT count(*)::int AS issues,
       count(*) FILTER (WHERE s.category = 'done')::int AS done_issues,
       coalesce(sum(i.story_points), 0)::float8 AS points,
       coalesce(sum(i.story_points) FILTER (WHERE s.category = 'done'), 0)::float8 AS done_points
FROM issues i JOIN statuses s ON s.id = i.status_id JOIN issue_types t ON t.id = i.type_id
WHERE i.sprint_id = $1 AND t.level >= 0;

-- ---------------------------------------------------------------------------
-- Releases

-- name: ListReleases :many
SELECT sqlc.embed(r),
    (SELECT count(*) FROM issue_releases ir WHERE ir.release_id = r.id)::int AS issue_count,
    (SELECT count(*) FROM issue_releases ir JOIN issues i ON i.id = ir.issue_id JOIN statuses s ON s.id = i.status_id
       WHERE ir.release_id = r.id AND s.category = 'done')::int AS done_count
FROM releases r WHERE r.project_id = $1 ORDER BY r.state = 'archived', r.position, r.created_at;

-- name: GetRelease :one
SELECT * FROM releases WHERE id = $1;

-- name: GetReleasesByIDs :many
SELECT * FROM releases WHERE id = ANY(@ids::uuid[]);

-- name: CreateRelease :one
INSERT INTO releases (project_id, name, description, start_date, release_date, position)
VALUES (@project_id, @name, @description, @start_date, @release_date,
        (SELECT coalesce(max(position), 0) + 1 FROM releases WHERE project_id = @project_id))
RETURNING *;

-- name: UpdateRelease :one
UPDATE releases SET name = $2, description = $3, start_date = $4, release_date = $5, state = $6,
    released_at = CASE WHEN $6 = 'released' THEN coalesce(released_at, now()) ELSE released_at END
WHERE id = $1 RETURNING *;

-- name: DeleteRelease :exec
DELETE FROM releases WHERE id = $1;

-- name: MoveOpenReleaseIssues :exec
-- Re-point unfinished issues from one release to another (or just remove them).
WITH moved AS (
    DELETE FROM issue_releases ir USING issues i, statuses s
    WHERE ir.release_id = @release_id AND i.id = ir.issue_id AND s.id = i.status_id AND s.category <> 'done'
    RETURNING ir.issue_id
)
INSERT INTO issue_releases (issue_id, release_id)
SELECT issue_id, sqlc.narg(target_release_id)::uuid FROM moved WHERE sqlc.narg(target_release_id)::uuid IS NOT NULL
ON CONFLICT DO NOTHING;

-- name: SetBoardCardFields :exec
UPDATE boards SET card_fields = @card_fields, updated_at = now() WHERE id = @id;

-- name: IssueHistoryFor :many
-- Status, sprint, story point and remaining estimate changes of many issues, oldest first (for metrics).
SELECT issue_id, field, from_ref, to_ref, from_value, to_value, created_at
FROM issue_history
WHERE issue_id = ANY(@ids::uuid[]) AND field IN ('status', 'sprint', 'storyPoints', 'remainingEstimate')
ORDER BY issue_id, created_at, id;

-- name: ClosedSprints :many
-- The most recently completed sprints of a project, newest first.
SELECT * FROM sprints WHERE project_id = $1 AND state = 'closed'
ORDER BY completed_at DESC NULLS LAST, position DESC
LIMIT $2;

-- name: IssuesEverInSprint :many
-- Issues in a sprint now, committed to it at the start, or moved in or out of it since.
SELECT id FROM issues WHERE sprint_id = @sprint_id
UNION
SELECT h.issue_id FROM issue_history h
WHERE h.field = 'sprint' AND (h.to_ref = @sprint_id::text OR h.from_ref = @sprint_id::text)
UNION
SELECT unnest(@committed::uuid[]);

-- name: ReleaseIDsOfIssues :many
SELECT issue_id, release_id FROM issue_releases WHERE issue_id = ANY(@ids::uuid[]);
