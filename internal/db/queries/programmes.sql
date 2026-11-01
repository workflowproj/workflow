-- name: ProgrammeProjects :many
-- The team projects of a programme, by name.
SELECT p.* FROM programme_projects pp JOIN projects p ON p.id = pp.project_id
WHERE pp.programme_id = $1 AND p.archived_at IS NULL
ORDER BY lower(p.name);

-- name: ProgrammesOfProject :many
-- The programmes a team project belongs to, by name.
SELECT p.* FROM programme_projects pp JOIN projects p ON p.id = pp.programme_id
WHERE pp.project_id = $1 AND p.archived_at IS NULL
ORDER BY lower(p.name);

-- name: IsProgrammeMember :one
SELECT EXISTS (SELECT 1 FROM programme_projects WHERE programme_id = @programme_id AND project_id = @project_id);

-- name: AddProgrammeProject :exec
INSERT INTO programme_projects (programme_id, project_id, added_by) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: RemoveProgrammeProject :execrows
DELETE FROM programme_projects WHERE programme_id = $1 AND project_id = $2;

-- name: UnlinkProgrammeEpics :many
-- Takes a team's epics out from under the programme's initiatives (when the team leaves it).
UPDATE issues u SET parent_id = NULL, version = u.version + 1, updated_at = now()
WHERE u.project_id = @team_id
  AND u.parent_id IN (SELECT x.id FROM issues x WHERE x.project_id = @programme_id)
RETURNING u.*;

-- name: CrossProjectChildren :many
-- Children in other projects of the given issues (a programme's initiatives' epics).
SELECT c.* FROM issues c JOIN issues p ON p.id = c.parent_id
WHERE c.parent_id = ANY(@ids::uuid[]) AND c.project_id <> p.project_id
ORDER BY c.project_id, c.number;

-- name: ProgrammeCrossProjectChildren :many
-- Children in other projects of any issue in a project (before the project is deleted).
SELECT c.* FROM issues c JOIN issues p ON p.id = c.parent_id
WHERE p.project_id = $1 AND c.project_id <> $1
ORDER BY c.project_id, c.number;

-- name: ChildProgressIn :many
-- Children of the given issues by status category, counting only children in the given projects.
SELECT i.parent_id, s.category, count(*)::int AS n, coalesce(sum(i.story_points), 0)::float8 AS points
FROM issues i JOIN statuses s ON s.id = i.status_id
WHERE i.parent_id = ANY(@ids::uuid[]) AND i.project_id = ANY(@project_ids::uuid[])
GROUP BY i.parent_id, s.category;

-- name: InitiativeProgress :many
-- The issues under initiatives' epics by status category, per initiative, counting only epics in
-- the given projects (an epic's issues are always in its own project).
SELECT e.parent_id AS initiative_id, s.category, count(*)::int AS n, coalesce(sum(i.story_points), 0)::float8 AS points
FROM issues e JOIN issues i ON i.parent_id = e.id JOIN statuses s ON s.id = i.status_id
WHERE e.parent_id = ANY(@ids::uuid[]) AND e.project_id = ANY(@project_ids::uuid[])
GROUP BY e.parent_id, s.category;

-- name: ChildLatestDue :many
-- The latest due date among each issue's children.
SELECT parent_id::uuid AS parent_id, max(due_date)::date AS latest_due
FROM issues WHERE parent_id = ANY(@ids::uuid[]) AND due_date IS NOT NULL
GROUP BY parent_id;

-- name: CrossProjectBlockers :many
-- Unfinished issues in other projects that block an epic or an issue under it.
SELECT t.epic_id::uuid AS epic_id, b.id AS blocker_id, b.project_id AS blocker_project_id
FROM (SELECT e.id AS epic_id, e.project_id, e.id AS issue_id FROM issues e WHERE e.id = ANY(@ids::uuid[])
      UNION ALL
      SELECT c.parent_id AS epic_id, c.project_id, c.id AS issue_id FROM issues c WHERE c.parent_id = ANY(@ids::uuid[])) t
JOIN issue_links l ON l.target_id = t.issue_id AND l.link_type = 'blocks'
JOIN issues b ON b.id = l.source_id
JOIN statuses bs ON bs.id = b.status_id
WHERE b.project_id <> t.project_id AND bs.category <> 'done'
ORDER BY b.project_id, b.number;

-- name: BlockLinksTouching :many
-- Blocks links with exactly one end among the given issues.
SELECT * FROM issue_links
WHERE link_type = 'blocks'
  AND ((source_id = ANY(@ids::uuid[]) AND NOT target_id = ANY(@ids::uuid[]))
    OR (target_id = ANY(@ids::uuid[]) AND NOT source_id = ANY(@ids::uuid[])))
ORDER BY created_at;
