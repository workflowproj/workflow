-- Deployments reported by GitHub and GitLab, and the issues each contains.

-- name: UpsertDeployment :one
-- Records a deployment's status. Reports can arrive out of order: an older status never
-- replaces a newer one.
INSERT INTO deployments (repository_id, external_id, environment, ref, sha, status, url, deployer, status_at)
VALUES (@repository_id, @external_id, @environment, @ref, @sha, @status, @url, @deployer, @status_at)
ON CONFLICT (repository_id, external_id) DO UPDATE SET
    status = CASE WHEN excluded.status_at >= deployments.status_at THEN excluded.status ELSE deployments.status END,
    url = CASE WHEN excluded.url <> '' AND excluded.status_at >= deployments.status_at THEN excluded.url ELSE deployments.url END,
    status_at = greatest(deployments.status_at, excluded.status_at),
    environment = CASE WHEN excluded.environment <> '' THEN excluded.environment ELSE deployments.environment END,
    ref = CASE WHEN excluded.ref <> '' THEN excluded.ref ELSE deployments.ref END,
    sha = CASE WHEN excluded.sha <> '' THEN excluded.sha ELSE deployments.sha END,
    deployer = CASE WHEN excluded.deployer <> '' THEN excluded.deployer ELSE deployments.deployer END
RETURNING *;

-- name: GetDeployment :one
SELECT * FROM deployments WHERE id = $1;

-- name: LockDeployment :one
SELECT * FROM deployments WHERE id = $1 FOR UPDATE;

-- name: PreviousSuccessfulDeployment :one
-- The last successful deployment to the same environment of the repository before this one.
SELECT * FROM deployments
WHERE repository_id = @repository_id AND lower(environment) = lower(@environment::text) AND status = 'success'
  AND id <> @id AND status_at < @before
ORDER BY status_at DESC
LIMIT 1;

-- name: IssuesLinkedToCommits :many
SELECT DISTINCT issue_id FROM dev_links
WHERE repository_id = @repository_id AND kind = 'commit' AND external_id = ANY(@shas::text[]);

-- name: AddDeploymentIssue :exec
INSERT INTO deployment_issues (deployment_id, issue_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: DeploymentIssueIDs :many
SELECT issue_id FROM deployment_issues WHERE deployment_id = $1;

-- name: MarkDeploymentResolved :exec
UPDATE deployments SET resolved_at = now(), base_sha = @base_sha, commit_count = @commit_count, truncated = @truncated
WHERE id = @id;

-- name: SetDeploymentNotified :exec
UPDATE deployments SET notified_status = @status WHERE id = @id;

-- name: IssueDeployments :many
-- Deployments containing an issue, newest first.
SELECT d.*, r.full_path, r.web_url AS repo_web_url, c.provider
FROM deployment_issues di
JOIN deployments d ON d.id = di.deployment_id
JOIN scm_repositories r ON r.id = d.repository_id
JOIN scm_connections c ON c.id = r.connection_id
WHERE di.issue_id = $1
ORDER BY d.status_at DESC
LIMIT 200;

-- name: IssueEnvironments :many
-- For each of the given issues, the environments it reached (a successful deployment contained
-- it) and when it first did.
SELECT di.issue_id, lower(d.environment)::text AS env_key, min(d.environment)::text AS environment,
    min(d.status_at)::timestamptz AS first_at, max(d.status_at)::timestamptz AS last_at
FROM deployment_issues di JOIN deployments d ON d.id = di.deployment_id
WHERE di.issue_id = ANY(@ids::uuid[]) AND d.status = 'success'
GROUP BY di.issue_id, lower(d.environment);

-- name: TagDeployments :many
-- Successful deployments of a tag (by name, or its commit when known), newest first.
SELECT * FROM deployments
WHERE repository_id = @repository_id AND status = 'success'
  AND (ref = @tag::text OR ref = 'refs/tags/' || @tag::text OR (@sha::text <> '' AND sha = @sha::text))
ORDER BY status_at DESC
LIMIT 100;
