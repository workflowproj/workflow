-- Import from Jira: import records, the Jira id mapping, placeholder people and the writes that
-- keep Jira's timestamps (issues, comments, history, attachments, sprints, releases).

-- name: CreateJiraImport :one
INSERT INTO jira_imports (flavor, base_url, site, email, token_enc, server_title, created_by)
VALUES (@flavor, @base_url, @site, @email, @token_enc, @server_title, @created_by)
RETURNING *;

-- name: GetJiraImport :one
SELECT * FROM jira_imports WHERE id = $1;

-- name: ListJiraImports :many
SELECT * FROM jira_imports ORDER BY created_at DESC LIMIT 100;

-- name: UpdateJiraImportConfig :one
UPDATE jira_imports SET config = @config, updated_at = now() WHERE id = @id RETURNING *;

-- name: UpdateJiraImportConnection :one
UPDATE jira_imports SET email = @email, token_enc = @token_enc, server_title = @server_title, updated_at = now()
WHERE id = @id RETURNING *;

-- name: ClearJiraImportCredentials :one
UPDATE jira_imports SET token_enc = NULL, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteJiraImport :exec
DELETE FROM jira_imports WHERE id = $1;

-- name: StartJiraImportRun :one
-- Starts a scan or import run; jobs carrying another run id stop.
UPDATE jira_imports SET status = @status, mode = @mode, run_id = @run_id, cursor = @cursor, progress = @progress,
    error = NULL, cancel_requested = false, started_at = now(), finished_at = NULL, heartbeat_at = now(), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SaveJiraImportStep :one
-- Checkpoints a run after a step; returns whether a cancel was requested.
UPDATE jira_imports SET cursor = @cursor, progress = @progress, scan = coalesce(sqlc.narg(scan), scan),
    status = coalesce(sqlc.narg(status), status), heartbeat_at = now(), updated_at = now()
WHERE id = @id AND run_id = @run_id
RETURNING cancel_requested;

-- name: FinishJiraImportRun :exec
UPDATE jira_imports SET status = @status, error = @error, progress = @progress, cursor = @cursor,
    finished_at = now(), heartbeat_at = now(), updated_at = now()
WHERE id = @id AND run_id = @run_id;

-- name: TouchJiraImportRun :exec
-- Heartbeat inside a long step (many attachments), so the run does not look stalled.
UPDATE jira_imports SET heartbeat_at = now() WHERE id = @id AND run_id = @run_id;

-- name: BusyJiraImportsOfSite :one
-- Other runs of the same site that are still checkpointing.
SELECT count(*)::int FROM jira_imports
WHERE site = @site AND id <> @id AND status IN ('scanning', 'running') AND heartbeat_at > @fresh_after;

-- name: RequestJiraImportCancel :one
UPDATE jira_imports SET cancel_requested = true, updated_at = now() WHERE id = $1 RETURNING *;

-- ---------------------------------------------------------------------------
-- Jira id mapping

-- name: GetJiraMap :one
SELECT * FROM jira_import_map WHERE site = @site AND kind = @kind AND jira_id = @jira_id;

-- name: UpsertJiraMap :exec
INSERT INTO jira_import_map (site, kind, jira_id, local_id, project_id, import_id, data)
VALUES (@site, @kind, @jira_id, @local_id, @project_id, @import_id, @data)
ON CONFLICT (site, kind, jira_id) DO UPDATE SET
    local_id = excluded.local_id, project_id = excluded.project_id, import_id = excluded.import_id,
    data = excluded.data, updated_at = now();

-- name: DeleteJiraMap :exec
DELETE FROM jira_import_map WHERE site = @site AND kind = @kind AND jira_id = @jira_id;

-- name: JiraMapPage :many
-- Mapped issues (or sprints) of one project in Jira id order, a page at a time.
SELECT * FROM jira_import_map
WHERE site = @site AND kind = @kind AND project_id = @project_id AND jira_id > @after::text
ORDER BY jira_id
LIMIT @lim;

-- name: JiraMapIssuesByRank :many
-- Imported issues of a project in Jira rank order (LexoRank strings sort bytewise).
SELECT m.local_id FROM jira_import_map m JOIN issues i ON i.id = m.local_id
WHERE m.site = @site AND m.kind = 'issue' AND m.project_id = @project_id
ORDER BY (m.data->>'rank') COLLATE "C" NULLS LAST, m.jira_id;

-- name: JiraMapLocalIDs :many
SELECT jira_id, local_id FROM jira_import_map WHERE site = @site AND kind = @kind AND jira_id = ANY(@jira_ids::text[]);

-- name: JiraMappedUsers :many
-- People mapped by earlier runs of the site, with whether they are still placeholders.
SELECT m.jira_id, u.id, u.email, u.display_name, u.active, (p.user_id IS NOT NULL)::bool AS placeholder
FROM jira_import_map m JOIN users u ON u.id = m.local_id LEFT JOIN user_placeholders p ON p.user_id = u.id
WHERE m.site = @site AND m.kind = 'user';

-- name: JiraMappedProjects :many
SELECT m.jira_id, p.id, p.key, p.name, p.template FROM jira_import_map m JOIN projects p ON p.id = m.local_id
WHERE m.site = @site AND m.kind = 'project';

-- name: UsersByEmails :many
SELECT * FROM users WHERE email = ANY(@emails::text[]);

-- ---------------------------------------------------------------------------
-- Placeholder people

-- name: CreateUserPlaceholder :exec
INSERT INTO user_placeholders (user_id, source, external_id) VALUES (@user_id, @source, @external_id)
ON CONFLICT (user_id) DO NOTHING;

-- name: IsUserPlaceholder :one
SELECT EXISTS (SELECT 1 FROM user_placeholders WHERE user_id = $1)::bool;

-- name: ClaimUserPlaceholder :one
-- The person behind a placeholder signed in: the account becomes theirs (the trigger on users
-- drops the placeholder marker).
UPDATE users SET active = true, deactivated_at = NULL, source = @source, updated_at = now()
WHERE id = @id AND NOT active AND deleted_at IS NULL
    AND EXISTS (SELECT 1 FROM user_placeholders p WHERE p.user_id = users.id)
RETURNING *;

-- ---------------------------------------------------------------------------
-- Writes that keep Jira's numbers and timestamps

-- name: ImportBumpProjectCounter :exec
-- Keeps the next issue number above every imported number.
UPDATE projects SET issue_counter = greatest(issue_counter, @number::int) WHERE id = @id;

-- name: ImportInsertIssue :one
INSERT INTO issues (id, project_id, number, type_id, parent_id, summary, description, status_id, workflow_version_id,
                    priority, assignee_id, reporter_id, sprint_id, story_points, start_date, due_date, labels,
                    resolution, resolved_at, fields, rank, created_at, updated_at, status_changed_at)
VALUES (@id, @project_id, @number, @type_id, @parent_id, @summary, @description, @status_id, @workflow_version_id,
        @priority, @assignee_id, @reporter_id, @sprint_id, @story_points, @start_date, @due_date, @labels,
        @resolution, @resolved_at, '{}', @rank, @created_at, @updated_at, @status_changed_at)
RETURNING *;

-- name: ImportUpdateIssue :one
UPDATE issues SET
    type_id = @type_id, summary = @summary, description = @description, status_id = @status_id,
    workflow_version_id = @workflow_version_id, priority = @priority, assignee_id = @assignee_id,
    reporter_id = @reporter_id, sprint_id = @sprint_id, story_points = @story_points, start_date = @start_date,
    due_date = @due_date, labels = @labels, resolution = @resolution, resolved_at = @resolved_at,
    created_at = @created_at, updated_at = @updated_at, version = version + 1
WHERE id = @id
RETURNING *;

-- name: ImportSetStatusChangedAt :exec
UPDATE issues SET status_changed_at = @status_changed_at WHERE id = @id;

-- name: ImportSetParent :exec
-- Sets the parent; sub-tasks take their parent's sprint.
UPDATE issues SET parent_id = @parent_id,
    sprint_id = CASE WHEN @subtask::bool THEN (SELECT p.sprint_id FROM issues p WHERE p.id = @parent_id) ELSE issues.sprint_id END
WHERE issues.id = @id;

-- name: ImportSetRank :exec
UPDATE issues SET rank = @rank WHERE id = @id;

-- name: ImportMaxRank :one
SELECT coalesce(max(rank), '')::text FROM issues WHERE project_id = $1;

-- name: ImportDeleteHistory :exec
DELETE FROM issue_history WHERE issue_id = $1 AND actor_type = 'import';

-- name: ImportInsertHistory :exec
INSERT INTO issue_history (issue_id, actor_id, actor_type, field, from_value, to_value, from_ref, to_ref, created_at)
VALUES (@issue_id, @actor_id, 'import', @field, @from_value, @to_value, @from_ref, @to_ref, @created_at);

-- name: ImportDeleteComments :exec
DELETE FROM comments WHERE issue_id = $1 AND source = 'import';

-- name: ImportInsertComment :exec
INSERT INTO comments (issue_id, author_id, body, source, edited_at, created_at)
VALUES (@issue_id, @author_id, @body, 'import', @edited_at, @created_at);

-- name: ImportInsertAttachment :one
INSERT INTO attachments (id, issue_id, uploader_id, filename, content_type, size_bytes, object_key, created_at)
VALUES (@id, @issue_id, @uploader_id, @filename, @content_type, @size_bytes, @object_key, @created_at)
RETURNING *;

-- name: ImportIssueAttachments :many
SELECT * FROM attachments WHERE issue_id = $1;

-- name: ImportLinkIssues :exec
INSERT INTO issue_links (source_id, target_id, link_type, created_by, created_at)
VALUES (@source_id, @target_id, @link_type, @created_by, @created_at)
ON CONFLICT (source_id, target_id, link_type) DO NOTHING;

-- name: ImportInsertSprint :one
INSERT INTO sprints (project_id, name, goal, state, start_date, end_date, completed_at, position)
VALUES (@project_id, @name, @goal, @state, @start_date, @end_date, @completed_at, @position)
RETURNING *;

-- name: ImportUpdateSprint :one
UPDATE sprints SET name = @name, goal = @goal, state = @state, start_date = @start_date, end_date = @end_date,
    completed_at = @completed_at, position = @position
WHERE id = @id
RETURNING *;

-- name: ImportSprintSnapshot :exec
UPDATE sprints SET committed_issue_ids = @committed_issue_ids, committed_points = @committed_points,
    completed_issue_ids = @completed_issue_ids
WHERE id = @id;

-- name: ImportDeleteEmptyFutureSprints :exec
-- Drops the template's empty first sprint once the import brings real ones.
DELETE FROM sprints s WHERE s.project_id = @project_id AND s.state = 'future' AND s.id <> ALL(@keep::uuid[])
    AND NOT EXISTS (SELECT 1 FROM issues i WHERE i.sprint_id = s.id);

-- name: ImportInsertRelease :one
INSERT INTO releases (project_id, name, description, start_date, release_date, state, released_at, position)
VALUES (@project_id, @name, @description, @start_date, @release_date, @state, @released_at, @position)
RETURNING *;

-- name: ImportUpdateRelease :one
UPDATE releases SET name = @name, description = @description, start_date = @start_date, release_date = @release_date,
    state = @state, released_at = @released_at, position = @position
WHERE id = @id
RETURNING *;

-- name: ImportReleaseByName :one
SELECT * FROM releases WHERE project_id = @project_id AND lower(name) = lower(@name);
