-- Bulk seed for the scale test: 20,000 issues per project with status, sprint and points
-- history, links, comments and releases. Run after setup.mjs.
\set ON_ERROR_STOP on

SELECT setseed(0.42);

-- Each type's workflow: version, and its first todo / in-progress / done status.
CREATE TEMP TABLE t_types AS
SELECT t.project_id, t.id AS type_id, t.key AS type_key, t.level, wv.id AS wv_id,
       (SELECT (s->>'id')::uuid FROM jsonb_array_elements(wv.definition->'statuses') WITH ORDINALITY x(s, o) WHERE s->>'category' = 'todo' ORDER BY o LIMIT 1) AS todo_id,
       (SELECT (s->>'id')::uuid FROM jsonb_array_elements(wv.definition->'statuses') WITH ORDINALITY x(s, o) WHERE s->>'category' = 'in_progress' ORDER BY o LIMIT 1) AS prog_id,
       (SELECT (s->>'id')::uuid FROM jsonb_array_elements(wv.definition->'statuses') WITH ORDINALITY x(s, o) WHERE s->>'category' = 'done' ORDER BY o LIMIT 1) AS done_id
FROM issue_types t
JOIN workflows w ON w.id = t.workflow_id
JOIN workflow_versions wv ON wv.workflow_id = w.id AND wv.version = w.current_version;

CREATE TEMP TABLE t_users AS SELECT id, row_number() OVER (ORDER BY email) - 1 AS n FROM users;
CREATE TEMP TABLE t_projects AS SELECT id, key, template FROM projects WHERE key IN ('SCRA', 'SCRB', 'SCRC', 'KANA', 'KANB');

-- Sprints: two weeks each, the last one active, the rest closed going back a year.
WITH s AS (
  SELECT sp.id, row_number() OVER (PARTITION BY sp.project_id ORDER BY sp.created_at, sp.name) AS k,
         count(*) OVER (PARTITION BY sp.project_id) AS total
  FROM sprints sp JOIN t_projects p ON p.id = sp.project_id
)
UPDATE sprints sp SET
  start_date = current_date - 7 - ((s.total - s.k) * 14)::int,
  end_date = current_date + 7 - ((s.total - s.k) * 14)::int,
  state = CASE WHEN s.k = s.total THEN 'active' ELSE 'closed' END,
  completed_at = CASE WHEN s.k = s.total THEN NULL ELSE (current_date + 7 - ((s.total - s.k) * 14)::int)::timestamptz END,
  position = s.k
FROM s WHERE s.id = sp.id;

-- Issues.
CREATE TEMP TABLE t_issues AS
WITH g AS (
  SELECT p.id AS project_id, p.key, p.template, n, random() AS r_type, random() AS r_state, random() AS r_age,
         random() AS r1, random() AS r2, random() AS r3, random() AS r4, random() AS r5
  FROM t_projects p, generate_series(5, 20004) n
)
SELECT gen_random_uuid() AS id, g.*,
       CASE WHEN n % 67 = 0 THEN 'epic' WHEN r_type < 0.28 THEN 'subtask'
            WHEN r_type < 0.64 THEN 'story' WHEN r_type < 0.86 THEN 'task' ELSE 'bug' END AS type_key,
       now() - (r_age * 365) * interval '1 day' AS created_at
FROM g;
-- Sub-tasks are "subtask" or "sub_task" depending on the template.
UPDATE t_issues i SET type_key = t.type_key
FROM t_types t WHERE i.type_key = 'subtask' AND t.project_id = i.project_id AND t.level = -1;

ALTER TABLE t_issues ADD COLUMN type_id uuid, ADD COLUMN level int, ADD COLUMN wv_id uuid,
  ADD COLUMN todo_id uuid, ADD COLUMN prog_id uuid, ADD COLUMN done_id uuid,
  ADD COLUMN state text, ADD COLUMN prog_at timestamptz, ADD COLUMN done_at timestamptz,
  ADD COLUMN parent_id uuid, ADD COLUMN sprint_id uuid, ADD COLUMN std_seq bigint;
UPDATE t_issues i SET type_id = t.type_id, level = t.level, wv_id = t.wv_id, todo_id = t.todo_id, prog_id = t.prog_id, done_id = t.done_id
FROM t_types t WHERE t.project_id = i.project_id AND t.type_key = i.type_key;

-- Older work is mostly done; the last month is mixed.
UPDATE t_issues SET state = CASE
  WHEN created_at < now() - interval '30 days' THEN CASE WHEN r_state < 0.9 THEN 'done' WHEN r_state < 0.91 THEN 'prog' ELSE 'todo' END
  ELSE CASE WHEN r_state < 0.3 THEN 'done' WHEN r_state < 0.55 THEN 'prog' ELSE 'todo' END END;
UPDATE t_issues SET prog_at = least(created_at + r1 * interval '5 days', now() - interval '2 hours') WHERE state IN ('prog', 'done');
UPDATE t_issues SET done_at = least(prog_at + (0.1 + r2 * 14) * interval '1 day', now() - interval '1 hour') WHERE state = 'done';

-- Parents: most standard issues sit under an epic; sub-tasks under the standard issue before them.
CREATE TEMP TABLE t_epics AS SELECT project_id, id, row_number() OVER (PARTITION BY project_id ORDER BY n) - 1 AS k, count(*) OVER (PARTITION BY project_id) AS total FROM t_issues WHERE level = 1;
UPDATE t_issues i SET parent_id = e.id FROM t_epics e
WHERE i.level = 0 AND i.r3 < 0.7 AND e.project_id = i.project_id AND e.k = floor(i.r4 * e.total)::int;
UPDATE t_issues i SET std_seq = s.seq FROM (
  SELECT id, count(*) FILTER (WHERE level = 0) OVER (PARTITION BY project_id ORDER BY n) AS seq FROM t_issues
) s WHERE s.id = i.id;
CREATE TEMP TABLE t_std AS SELECT project_id, std_seq, id, created_at FROM t_issues WHERE level = 0;
CREATE INDEX ON t_std (project_id, std_seq);
UPDATE t_issues i SET parent_id = s.id FROM t_std s
WHERE i.level = -1 AND s.project_id = i.project_id AND s.std_seq = greatest(i.std_seq, 1);

-- Sprints (scrum): done work in the sprint it finished in, open work in the active sprint
-- (in progress) or the backlog; sub-tasks follow their parent.
CREATE TEMP TABLE t_sprints AS SELECT id, project_id, start_date, end_date, state FROM sprints WHERE project_id IN (SELECT id FROM t_projects WHERE template = 'scrum');
UPDATE t_issues i SET sprint_id = s.id FROM t_sprints s
WHERE i.template = 'scrum' AND i.level = 0 AND s.project_id = i.project_id AND (
  (i.state = 'done' AND i.done_at::date >= s.start_date AND i.done_at::date < s.end_date) OR
  (i.state = 'prog' AND s.state = 'active') OR
  (i.state = 'todo' AND i.r5 < 0.15 AND s.state = 'active' AND i.created_at > now() - interval '30 days'));
UPDATE t_issues i SET sprint_id = p.sprint_id FROM t_issues p WHERE i.level = -1 AND p.id = i.parent_id;

INSERT INTO issues (id, project_id, number, type_id, parent_id, summary, description, status_id, workflow_version_id,
                    priority, assignee_id, reporter_id, sprint_id, story_points, start_date, due_date, labels,
                    resolution, resolved_at, rank, created_at, updated_at, status_changed_at)
SELECT i.id, i.project_id, i.n, i.type_id, i.parent_id,
       (ARRAY['Improve', 'Fix', 'Add', 'Refactor', 'Document', 'Speed up', 'Remove', 'Support'])[1 + floor(i.r1 * 8)::int] || ' ' ||
       (ARRAY['checkout', 'search', 'login', 'billing', 'export', 'reports', 'onboarding', 'notifications', 'the API', 'mobile layout'])[1 + floor(i.r2 * 10)::int] ||
       ' for ' || (ARRAY['admins', 'new customers', 'the EU region', 'large accounts', 'partners', 'support'])[1 + floor(i.r3 * 6)::int] || ' #' || i.n,
       repeat('Context and acceptance criteria for this change. ', 1 + floor(i.r4 * 6)::int),
       CASE i.state WHEN 'done' THEN i.done_id WHEN 'prog' THEN i.prog_id ELSE i.todo_id END,
       i.wv_id,
       (ARRAY['highest', 'high', 'medium', 'medium', 'medium', 'low', 'lowest'])[1 + floor(i.r5 * 7)::int],
       CASE WHEN i.r2 < 0.1 THEN NULL ELSE (SELECT id FROM t_users WHERE n = floor(i.r3 * 30)::int) END,
       (SELECT id FROM t_users WHERE n = floor(i.r4 * 30)::int),
       i.sprint_id,
       CASE WHEN i.type_key = 'story' THEN (ARRAY[1, 2, 3, 5, 8])[1 + floor(i.r1 * 5)::int] END,
       CASE WHEN i.level = 1 THEN i.created_at::date END,
       CASE WHEN i.level = 1 THEN (i.created_at + interval '60 days')::date WHEN i.r5 < 0.1 THEN (i.created_at + interval '14 days')::date END,
       CASE WHEN i.r3 < 0.5 THEN '{}'::text[] ELSE ARRAY[(ARRAY['frontend', 'backend', 'api', 'infra', 'ux', 'perf'])[1 + floor(i.r4 * 6)::int]] END,
       CASE WHEN i.state = 'done' THEN 'Done' END,
       i.done_at,
       'b' || lpad(i.n::text, 6, '0'),
       i.created_at,
       coalesce(i.done_at, i.prog_at, i.created_at),
       coalesce(i.done_at, i.prog_at, i.created_at)
FROM t_issues i;
UPDATE projects p SET issue_counter = 20004 FROM t_projects t WHERE t.id = p.id;

-- History: created, status changes, sprint and story points.
INSERT INTO issue_history (issue_id, actor_id, field, to_value, created_at)
SELECT i.id, (SELECT id FROM t_users WHERE n = floor(i.r4 * 30)::int), 'created', 'seeded', i.created_at FROM t_issues i;
INSERT INTO issue_history (issue_id, actor_id, field, from_value, to_value, from_ref, to_ref, created_at)
SELECT i.id, (SELECT id FROM t_users WHERE n = floor(i.r3 * 30)::int), 'status', 'To Do', 'In Progress', i.todo_id::text, i.prog_id::text, i.prog_at
FROM t_issues i WHERE i.prog_at IS NOT NULL;
INSERT INTO issue_history (issue_id, actor_id, field, from_value, to_value, from_ref, to_ref, created_at)
SELECT i.id, (SELECT id FROM t_users WHERE n = floor(i.r3 * 30)::int), 'status', 'In Progress', 'Done', i.prog_id::text, i.done_id::text, i.done_at
FROM t_issues i WHERE i.done_at IS NOT NULL;
INSERT INTO issue_history (issue_id, actor_id, field, from_value, to_value, from_ref, to_ref, created_at)
SELECT i.id, NULL, 'sprint', NULL, sp.name, NULL, sp.id::text, greatest(i.created_at, sp.start_date::timestamptz)
FROM t_issues i JOIN sprints sp ON sp.id = i.sprint_id;
INSERT INTO issue_history (issue_id, actor_id, field, from_value, to_value, created_at)
SELECT i.id, NULL, 'storyPoints', NULL, x.story_points::text, i.created_at
FROM t_issues i JOIN issues x ON x.id = i.id WHERE x.story_points IS NOT NULL;

-- Closed sprints remember what was committed and completed.
UPDATE sprints sp SET committed_issue_ids = x.ids, committed_points = x.pts, completed_issue_ids = x.done
FROM (
  SELECT i.sprint_id, array_agg(i.id) AS ids, coalesce(sum(i.story_points), 0) AS pts,
         array_agg(i.id) FILTER (WHERE i.resolved_at IS NOT NULL) AS done
  FROM issues i JOIN t_sprints s ON s.id = i.sprint_id GROUP BY i.sprint_id
) x WHERE x.sprint_id = sp.id AND sp.state = 'closed';

-- Links, comments, releases.
INSERT INTO issue_links (source_id, target_id, link_type, created_at)
SELECT DISTINCT ON (a.id, b.id) a.id, b.id, CASE WHEN a.r1 < 0.6 THEN 'blocks' ELSE 'relates' END, a.created_at
FROM t_issues a JOIN t_issues b ON b.project_id = a.project_id AND b.n = a.n + 1 + floor(a.r2 * 40)::int
WHERE a.r5 < 0.15 AND a.level = 0
ON CONFLICT DO NOTHING;
INSERT INTO comments (issue_id, author_id, body, created_at)
SELECT i.id, (SELECT id FROM t_users WHERE n = (floor(i.r1 * 30)::int + c) % 30), 'Update ' || c || ': progress notes and a question about the approach.', i.created_at + c * interval '1 hour'
FROM t_issues i, generate_series(1, 3) c WHERE i.r2 < 0.2 + c * 0.05;
INSERT INTO issue_releases (issue_id, release_id)
SELECT i.id, r.id FROM t_issues i
JOIN LATERAL (SELECT id FROM releases WHERE project_id = i.project_id ORDER BY name OFFSET floor(i.r4 * 10)::int LIMIT 1) r ON true
WHERE i.level = 0 AND i.r5 < 0.5;

ANALYZE;
SELECT p.key, count(*) AS issues, count(*) FILTER (WHERE i.resolved_at IS NULL) AS open,
       count(*) FILTER (WHERE s.state = 'active') AS in_active_sprint
FROM issues i JOIN projects p ON p.id = i.project_id LEFT JOIN sprints s ON s.id = i.sprint_id GROUP BY 1 ORDER BY 1;
SELECT (SELECT count(*) FROM issue_history) AS history, (SELECT count(*) FROM comments) AS comments, (SELECT count(*) FROM issue_links) AS links;
