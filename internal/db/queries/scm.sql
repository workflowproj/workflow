-- name: ListScmConnections :many
SELECT c.*, (SELECT count(*) FROM scm_repositories r WHERE r.connection_id = c.id)::int AS repository_count
FROM scm_connections c ORDER BY c.created_at;

-- name: GetScmConnection :one
SELECT * FROM scm_connections WHERE id = $1;

-- name: CreateScmConnection :one
INSERT INTO scm_connections (provider, name, api_url, web_url, app_id, app_slug, secrets_enc, manage_hooks, write_back, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpdateScmConnection :one
UPDATE scm_connections SET name = $2, secrets_enc = $3, manage_hooks = $4, write_back = $5, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: DeleteScmConnection :exec
DELETE FROM scm_connections WHERE id = $1;

-- name: TouchScmConnection :exec
UPDATE scm_connections SET last_event_at = now() WHERE id = $1;

-- name: UpsertScmRepository :one
INSERT INTO scm_repositories (connection_id, external_id, full_path, web_url, default_branch, installation_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (connection_id, external_id) DO UPDATE SET
    full_path = excluded.full_path, web_url = excluded.web_url, default_branch = excluded.default_branch,
    installation_id = coalesce(excluded.installation_id, scm_repositories.installation_id), updated_at = now()
RETURNING *;

-- name: GetScmRepository :one
SELECT * FROM scm_repositories WHERE id = $1;

-- name: GetScmRepositoryByExternalID :one
SELECT * FROM scm_repositories WHERE connection_id = $1 AND external_id = $2;

-- name: GetScmRepositoryByPath :one
SELECT * FROM scm_repositories WHERE connection_id = $1 AND lower(full_path) = lower(@full_path::text);

-- name: SetRepositoryHook :exec
UPDATE scm_repositories SET hook_id = $2, updated_at = now() WHERE id = $1;

-- name: DeleteScmRepository :exec
DELETE FROM scm_repositories WHERE id = $1;

-- name: ListScmRepositories :many
SELECT r.*, c.provider,
    coalesce((SELECT array_agg(p.key ORDER BY p.key) FROM project_repositories pr JOIN projects p ON p.id = pr.project_id
              WHERE pr.repository_id = r.id), '{}')::text[] AS project_keys
FROM scm_repositories r JOIN scm_connections c ON c.id = r.connection_id
WHERE @q::text = '' OR r.full_path ILIKE '%' || @q || '%'
ORDER BY r.full_path
LIMIT 500;

-- name: ListConnectionRepositories :many
SELECT * FROM scm_repositories WHERE connection_id = $1 ORDER BY full_path;

-- name: ListProjectRepositories :many
SELECT r.*, c.provider,
    coalesce((SELECT array_agg(p.key ORDER BY p.key) FROM project_repositories pr2 JOIN projects p ON p.id = pr2.project_id
              WHERE pr2.repository_id = r.id), '{}')::text[] AS project_keys
FROM project_repositories pr
JOIN scm_repositories r ON r.id = pr.repository_id
JOIN scm_connections c ON c.id = r.connection_id
WHERE pr.project_id = $1
ORDER BY r.full_path;

-- name: LinkProjectRepository :exec
INSERT INTO project_repositories (project_id, repository_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: UnlinkProjectRepository :exec
DELETE FROM project_repositories WHERE project_id = $1 AND repository_id = $2;

-- name: RepositoryProjects :many
-- Projects connected to a repository: only their keys are linked from Git activity.
SELECT p.* FROM projects p JOIN project_repositories pr ON pr.project_id = p.id WHERE pr.repository_id = $1;

-- name: LinkedRepositories :many
SELECT DISTINCT r.* FROM scm_repositories r JOIN project_repositories pr ON pr.repository_id = r.id
WHERE r.connection_id = $1;

-- ---------------------------------------------------------------------------
-- Inbound webhook events (store, then process)

-- name: InsertWebhookEvent :one
INSERT INTO webhook_events (connection_id, provider, event_type, delivery_id, payload)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (connection_id, delivery_id) DO NOTHING
RETURNING id;

-- name: GetWebhookEvent :one
SELECT * FROM webhook_events WHERE id = $1;

-- name: FinishWebhookEvent :exec
UPDATE webhook_events SET status = $2, error = $3, attempts = attempts + 1, processed_at = now() WHERE id = $1;

-- name: ResetWebhookEvent :exec
UPDATE webhook_events SET status = 'pending', error = NULL WHERE id = $1;

-- name: ListWebhookEvents :many
SELECT * FROM webhook_events WHERE connection_id = $1 ORDER BY received_at DESC LIMIT $2;

-- name: PruneWebhookEvents :execrows
DELETE FROM webhook_events WHERE received_at < $1 AND status <> 'failed';

-- ---------------------------------------------------------------------------
-- Dev links

-- name: UpsertDevLink :one
INSERT INTO dev_links (issue_id, repository_id, kind, external_id, title, url, state, author_name, author_email, data, created_at, updated_at)
VALUES (@issue_id, @repository_id, @kind, @external_id, @title, @url, @state, @author_name, @author_email, @data, @event_at, @event_at)
ON CONFLICT (issue_id, repository_id, kind, external_id) DO UPDATE SET
    title = excluded.title,
    url = CASE WHEN excluded.url <> '' THEN excluded.url ELSE dev_links.url END,
    state = CASE WHEN excluded.state <> '' THEN excluded.state ELSE dev_links.state END,
    author_name = CASE WHEN excluded.author_name <> '' THEN excluded.author_name ELSE dev_links.author_name END,
    author_email = CASE WHEN excluded.author_email <> '' THEN excluded.author_email ELSE dev_links.author_email END,
    data = dev_links.data || excluded.data,
    updated_at = greatest(dev_links.updated_at, excluded.updated_at)
RETURNING *, (xmax = 0) AS inserted;

-- name: UpdateDevLinksState :exec
-- Updates every issue's copy of an artefact (e.g. a PR merged or a branch deleted).
UPDATE dev_links SET state = @state, data = data || @data, updated_at = now()
WHERE repository_id = @repository_id AND kind = @kind AND external_id = @external_id;

-- name: MergeDevLinkData :exec
UPDATE dev_links SET data = data || @data WHERE id = @id;

-- name: IssueDevLinks :many
SELECT d.*, r.full_path, r.web_url AS repo_web_url, c.provider
FROM dev_links d JOIN scm_repositories r ON r.id = d.repository_id JOIN scm_connections c ON c.id = r.connection_id
WHERE d.issue_id = $1
ORDER BY d.updated_at DESC;

-- name: DevSummaries :many
SELECT issue_id,
    count(*) FILTER (WHERE kind = 'branch' AND state <> 'deleted')::int AS branches,
    count(*) FILTER (WHERE kind = 'commit')::int AS commits,
    count(*) FILTER (WHERE kind = 'pull_request')::int AS pull_requests,
    count(*) FILTER (WHERE kind = 'pull_request' AND state IN ('open', 'draft'))::int AS open_pull_requests,
    count(*) FILTER (WHERE kind = 'pull_request' AND state = 'merged')::int AS merged_pull_requests,
    coalesce((array_agg(state ORDER BY updated_at DESC) FILTER (WHERE kind = 'pipeline'))[1], '')::text AS pipeline_state
FROM dev_links WHERE issue_id = ANY(@ids::uuid[])
GROUP BY issue_id;

-- name: IssueDevLinksForRepo :many
SELECT * FROM dev_links WHERE repository_id = $1 AND kind = $2 AND external_id = $3;

-- name: GetDevLink :one
SELECT * FROM dev_links WHERE id = $1;
