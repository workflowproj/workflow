-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key = upper(@key::text);

-- name: GetProjectByAlias :one
SELECT p.* FROM projects p JOIN project_key_aliases a ON a.project_id = p.id WHERE a.key = upper(@key::text);

-- name: ListProjects :many
SELECT * FROM projects WHERE archived_at IS NULL ORDER BY name;

-- name: ListProjectsByIDs :many
SELECT * FROM projects WHERE id = ANY(@ids::uuid[]);

-- name: ProjectKeyAliases :many
SELECT key FROM project_key_aliases WHERE project_id = $1 ORDER BY created_at;

-- name: CreateProject :one
INSERT INTO projects (key, name, description, lead_id, template, open_role_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateProject :one
UPDATE projects SET key = @key, name = @name, description = @description, lead_id = @lead_id,
    open_role_id = @open_role_id, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: AddProjectKeyAlias :exec
INSERT INTO project_key_aliases (key, project_id) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET project_id = excluded.project_id;

-- name: DeleteProjectKeyAlias :exec
DELETE FROM project_key_aliases WHERE key = $1;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;

-- name: LockProjectCounter :one
-- The per-project issue counter row, locked for the rest of the transaction.
SELECT issue_counter FROM projects WHERE id = $1 FOR UPDATE;

-- name: SetProjectCounter :exec
UPDATE projects SET issue_counter = $2 WHERE id = $1;

-- name: ProjectLabels :many
SELECT DISTINCT unnest(labels)::text AS label FROM issues WHERE project_id = $1 ORDER BY 1;

-- ---------------------------------------------------------------------------
-- Membership & permissions

-- name: ListProjectMembers :many
SELECT pm.id, pm.project_id, pm.user_id, pm.group_id, pm.role_id, pm.created_at,
       sqlc.embed(r)
FROM project_members pm JOIN roles r ON r.id = pm.role_id
WHERE pm.project_id = $1
ORDER BY pm.created_at;

-- name: GetProjectMember :one
SELECT * FROM project_members WHERE id = $1 AND project_id = $2;

-- name: AddProjectMember :one
INSERT INTO project_members (project_id, user_id, group_id, role_id) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateProjectMemberRole :one
UPDATE project_members SET role_id = $3 WHERE id = $1 AND project_id = $2 RETURNING *;

-- name: RemoveProjectMember :exec
DELETE FROM project_members WHERE id = $1 AND project_id = $2;

-- name: UserProjectRoles :many
-- Every role a user holds in a project, directly or through groups. Effective rights are the union.
SELECT r.key, r.permissions FROM project_members pm
JOIN roles r ON r.id = pm.role_id
WHERE pm.project_id = @project_id
  AND (pm.user_id = @user_id::uuid OR pm.group_id IN (SELECT group_id FROM group_members WHERE user_id = @user_id::uuid));

-- name: UserMemberProjectIDs :many
SELECT DISTINCT pm.project_id FROM project_members pm
WHERE pm.user_id = @user_id::uuid OR pm.group_id IN (SELECT group_id FROM group_members WHERE user_id = @user_id::uuid);

-- ---------------------------------------------------------------------------
-- Statuses & workflows

-- name: ListStatuses :many
SELECT * FROM statuses ORDER BY name;

-- name: GetStatusesByIDs :many
SELECT * FROM statuses WHERE id = ANY(@ids::uuid[]);

-- name: GetStatusByName :one
SELECT * FROM statuses WHERE lower(name) = lower(@name::text);

-- name: CreateStatus :one
INSERT INTO statuses (name, category) VALUES ($1, $2) RETURNING *;

-- name: GetWorkflow :one
SELECT * FROM workflows WHERE id = $1;

-- name: GetWorkflowForUpdate :one
SELECT * FROM workflows WHERE id = $1 FOR UPDATE;

-- name: GetWorkflowByName :one
SELECT * FROM workflows
WHERE coalesce(project_id, '00000000-0000-0000-0000-000000000000'::uuid) = coalesce(sqlc.narg(project_id)::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND lower(name) = lower(@name::text);

-- name: ListWorkflows :many
SELECT * FROM workflows
WHERE (sqlc.narg(project_id)::uuid IS NULL AND project_id IS NULL)
   OR project_id = sqlc.narg(project_id) OR project_id IS NULL
ORDER BY project_id NULLS LAST, name;

-- name: ListAllWorkflows :many
SELECT * FROM workflows ORDER BY name;

-- name: CreateWorkflow :one
INSERT INTO workflows (project_id, name, description) VALUES ($1, $2, $3) RETURNING *;

-- name: SetWorkflowVersion :exec
UPDATE workflows SET current_version = @current_version, description = @description, updated_at = now() WHERE id = @id;

-- name: DeleteWorkflow :exec
DELETE FROM workflows WHERE id = $1;

-- name: CreateWorkflowVersion :one
INSERT INTO workflow_versions (workflow_id, version, definition, source_yaml, published_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetWorkflowVersion :one
SELECT * FROM workflow_versions WHERE workflow_id = $1 AND version = $2;

-- name: GetWorkflowVersionByID :one
SELECT * FROM workflow_versions WHERE id = $1;

-- name: GetWorkflowVersionsByIDs :many
SELECT * FROM workflow_versions WHERE id = ANY(@ids::uuid[]);

-- name: ListWorkflowVersions :many
SELECT wv.id, wv.workflow_id, wv.version, wv.published_by, wv.published_at,
       (SELECT count(*) FROM issues i WHERE i.workflow_version_id = wv.id)::int AS issue_count
FROM workflow_versions wv WHERE wv.workflow_id = $1 ORDER BY wv.version DESC;

-- name: WorkflowStatusUsage :many
-- Issues per status across every version of a workflow (used to require status mappings).
SELECT i.status_id, count(*)::int AS n FROM issues i
JOIN workflow_versions wv ON wv.id = i.workflow_version_id
WHERE wv.workflow_id = $1
GROUP BY i.status_id;

-- name: WorkflowUsage :many
SELECT p.key AS project_key, it.key AS issue_type_key FROM issue_types it
JOIN projects p ON p.id = it.project_id
WHERE it.workflow_id = $1 ORDER BY p.key, it.position;

-- name: PendingMigrationCount :one
SELECT count(*) FROM issues i
JOIN workflow_versions wv ON wv.id = i.workflow_version_id
JOIN workflows w ON w.id = wv.workflow_id
WHERE w.id = $1 AND wv.version <> w.current_version;

-- name: IssuesToMigrate :many
-- A batch of issues whose type maps to the workflow but which are not on its current version.
SELECT i.id, i.status_id, i.workflow_version_id FROM issues i
JOIN issue_types it ON it.id = i.type_id
WHERE it.workflow_id = @workflow_id AND i.workflow_version_id <> @version_id
ORDER BY i.id
LIMIT @lim
FOR UPDATE OF i SKIP LOCKED;

-- name: RepinIssue :exec
-- Resolution follows the (possibly new) status category.
UPDATE issues SET workflow_version_id = @workflow_version_id, status_id = @status_id,
    resolution = CASE WHEN (SELECT st.category FROM statuses st WHERE st.id = @status_id::uuid) = 'done' THEN coalesce(resolution, 'Done') ELSE NULL END,
    resolved_at = CASE WHEN (SELECT st.category FROM statuses st WHERE st.id = @status_id::uuid) = 'done' THEN coalesce(resolved_at, now()) ELSE NULL END,
    version = version + 1, updated_at = now()
WHERE issues.id = @id::uuid;

-- ---------------------------------------------------------------------------
-- Issue types

-- name: ListIssueTypes :many
SELECT it.*, w.name AS workflow_name FROM issue_types it JOIN workflows w ON w.id = it.workflow_id
WHERE it.project_id = $1 ORDER BY it.level DESC, it.position;

-- name: GetIssueType :one
SELECT * FROM issue_types WHERE id = $1;

-- name: GetIssueTypesByIDs :many
SELECT * FROM issue_types WHERE id = ANY(@ids::uuid[]);

-- name: GetIssueTypeByKey :one
SELECT * FROM issue_types WHERE project_id = $1 AND key = $2;

-- name: CreateIssueType :one
INSERT INTO issue_types (project_id, key, name, description, icon, color, level, position, workflow_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateIssueType :one
UPDATE issue_types SET name = @name, description = @description, icon = @icon, color = @color, workflow_id = @workflow_id
WHERE id = @id
RETURNING *;

-- name: DeleteIssueType :exec
DELETE FROM issue_types WHERE id = $1;

-- name: CountIssuesOfType :one
SELECT count(*) FROM issues WHERE type_id = $1;

-- name: ProjectStatusIDs :many
-- Every status referenced by the current versions of the project's workflows.
SELECT DISTINCT (s->>'id')::uuid AS status_id
FROM issue_types it
JOIN workflows w ON w.id = it.workflow_id
JOIN workflow_versions wv ON wv.workflow_id = w.id AND wv.version = w.current_version
CROSS JOIN LATERAL jsonb_array_elements(wv.definition->'statuses') s
WHERE it.project_id = $1;

-- ---------------------------------------------------------------------------
-- Custom fields

-- name: ListFieldDefinitions :many
SELECT * FROM field_definitions ORDER BY name;

-- name: GetFieldDefinition :one
SELECT * FROM field_definitions WHERE id = $1;

-- name: CreateFieldDefinition :one
INSERT INTO field_definitions (key, name, description, type, options, project_ids, issue_type_keys)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateFieldDefinition :one
UPDATE field_definitions SET key = @key, name = @name, description = @description, type = @type, options = @options,
    project_ids = @project_ids, issue_type_keys = @issue_type_keys, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteFieldDefinition :exec
DELETE FROM field_definitions WHERE id = $1;
