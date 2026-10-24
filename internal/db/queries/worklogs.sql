-- name: CreateWorklog :one
INSERT INTO worklogs (issue_id, author_id, started_at, duration_seconds, comment)
VALUES (@issue_id, @author_id, @started_at, @duration_seconds, @comment)
RETURNING *;

-- name: GetWorklog :one
SELECT * FROM worklogs WHERE id = $1;

-- name: UpdateWorklog :one
UPDATE worklogs SET started_at = @started_at, duration_seconds = @duration_seconds, comment = @comment, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteWorklog :exec
DELETE FROM worklogs WHERE id = $1;

-- name: ListWorklogs :many
SELECT * FROM worklogs WHERE issue_id = $1 ORDER BY started_at, created_at;

-- name: IssueTimeSpent :many
-- Time spent per issue: the sum of its worklogs (issues without any are left out).
SELECT issue_id, sum(duration_seconds)::bigint AS seconds
FROM worklogs
WHERE issue_id = ANY(@ids::uuid[])
GROUP BY issue_id;

-- name: SetRemainingEstimate :one
-- Logging work changes what the issue shows (time spent, maybe the remaining estimate), so it
-- bumps the version like any other write.
UPDATE issues SET remaining_estimate_seconds = @remaining_estimate_seconds, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: WorklogsOfIssues :many
-- Worklogs of many issues in the order they were logged (sprint reports in hours).
SELECT issue_id, started_at, created_at, duration_seconds
FROM worklogs
WHERE issue_id = ANY(@ids::uuid[])
ORDER BY created_at;

-- name: TimesheetRows :many
-- Time logged per person, issue and day (in the given time zone) on issues of the projects,
-- for days from..to inclusive; optionally one person's only.
SELECT w.author_id, w.issue_id, (w.started_at AT TIME ZONE @tz::text)::date AS day, sum(w.duration_seconds)::bigint AS seconds
FROM worklogs w
JOIN issues i ON i.id = w.issue_id
WHERE i.project_id = ANY(@project_ids::uuid[])
  AND w.started_at >= ((@from_day::date)::timestamp AT TIME ZONE @tz::text)
  AND w.started_at < (((@to_day::date) + 1)::timestamp AT TIME ZONE @tz::text)
  AND (sqlc.narg(author_id)::uuid IS NULL OR w.author_id = sqlc.narg(author_id)::uuid)
GROUP BY w.author_id, w.issue_id, day
ORDER BY day;
