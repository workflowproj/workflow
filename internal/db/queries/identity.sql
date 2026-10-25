-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = lower(@email::text);

-- name: GetUserByGoogleID :one
SELECT * FROM users WHERE google_id = $1;

-- name: GetUsersByIDs :many
SELECT * FROM users WHERE id = ANY(@ids::uuid[]);

-- name: CountUsers :one
SELECT count(*) FROM users WHERE kind = 'human';

-- name: CreateUser :one
INSERT INTO users (kind, email, username, display_name, given_name, family_name, avatar_url, org_role, source,
                   google_id, scim_connection_id, scim_external_id, active)
VALUES (@kind, lower(@email::text), @username, @display_name, @given_name, @family_name, @avatar_url, @org_role, @source,
        @google_id, @scim_connection_id, @scim_external_id, @active)
RETURNING *;

-- name: UpdateUserProfile :one
UPDATE users SET
    display_name = @display_name,
    given_name   = @given_name,
    family_name  = @family_name,
    avatar_url   = coalesce(@avatar_url, avatar_url),
    updated_at   = now()
WHERE id = @id
RETURNING *;

-- name: SetUserOrgRole :one
UPDATE users SET org_role = @org_role, updated_at = now() WHERE id = @id RETURNING *;

-- name: SetUserActive :one
UPDATE users SET
    active = @active,
    deactivated_at = CASE WHEN @active::bool THEN NULL ELSE coalesce(deactivated_at, now()) END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: LinkGoogleID :exec
UPDATE users SET google_id = @google_id, updated_at = now() WHERE id = @id;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now() WHERE id = $1;

-- name: SearchUsers :many
SELECT * FROM users
WHERE active AND deleted_at IS NULL
  AND (@q::text = '' OR display_name ILIKE '%' || @q || '%' OR email ILIKE '%' || @q || '%')
ORDER BY display_name
LIMIT @lim;

-- name: AdminListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
  AND (@q::text = '' OR display_name ILIKE '%' || @q || '%' OR email ILIKE '%' || @q || '%')
  AND (@kind::text = '' OR kind = @kind)
ORDER BY kind, display_name;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE org_role = 'admin' AND active AND deleted_at IS NULL AND id <> @exclude_id;

-- name: EffectiveOrgRoles :many
-- Org roles granted through group membership.
SELECT DISTINCT g.org_role::text AS org_role FROM groups g
JOIN group_members gm ON gm.group_id = g.id
WHERE gm.user_id = $1 AND g.org_role IS NOT NULL;

-- name: MarkUserDeleted :exec
UPDATE users SET active = false, deactivated_at = coalesce(deactivated_at, now()), deleted_at = coalesce(deleted_at, now()), updated_at = now()
WHERE id = $1;

-- name: UsersToAnonymize :many
SELECT id FROM users WHERE deleted_at IS NOT NULL AND anonymized_at IS NULL AND deleted_at < @before;

-- name: AnonymizeUser :exec
UPDATE users SET
    email = 'deleted-' || id::text || '@invalid',
    username = 'deleted-' || id::text,
    display_name = 'Former user',
    given_name = NULL, family_name = NULL, avatar_url = NULL,
    google_id = NULL, scim_external_id = NULL,
    anonymized_at = now(), updated_at = now()
WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Federated identities & sessions

-- name: GetIdentity :one
SELECT * FROM user_identities WHERE provider = $1 AND subject = $2;

-- name: UpsertIdentity :exec
INSERT INTO user_identities (user_id, provider, subject, email, last_used_at)
VALUES (@user_id, @provider, @subject, @email, now())
ON CONFLICT (provider, subject) DO UPDATE SET email = excluded.email, last_used_at = now();

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, expires_at, ip, user_agent, idp_provider, idp_subject, idp_sid, idp_id_token)
VALUES (@id, @user_id, @expires_at, @ip, @user_agent, sqlc.narg(idp_provider), sqlc.narg(idp_subject), sqlc.narg(idp_sid), sqlc.narg(idp_id_token));

-- name: GetSessionIDP :one
SELECT idp_provider, idp_id_token FROM sessions WHERE id = $1;

-- name: DeleteSessionsByIDPSid :execrows
DELETE FROM sessions WHERE idp_provider = @provider AND idp_sid = @sid;

-- name: DeleteSessionsByIDPSubject :execrows
DELETE FROM sessions WHERE idp_provider = @provider AND idp_subject = @subject;

-- name: GetSessionUser :one
SELECT sqlc.embed(users), sessions.last_seen_at AS session_last_seen
FROM sessions JOIN users ON users.id = sessions.user_id
WHERE sessions.id = $1 AND sessions.expires_at > now() AND users.active AND users.deleted_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = now() WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < now();

-- ---------------------------------------------------------------------------
-- API tokens

-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, scopes, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetAPITokenUser :one
SELECT sqlc.embed(api_tokens), sqlc.embed(users)
FROM api_tokens JOIN users ON users.id = api_tokens.user_id
WHERE api_tokens.token_hash = $1 AND api_tokens.revoked_at IS NULL AND api_tokens.expires_at > now()
  AND users.active AND users.deleted_at IS NULL;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = now() WHERE id = $1;

-- name: ListUserTokens :many
SELECT * FROM api_tokens WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC;

-- name: GetAPIToken :one
SELECT * FROM api_tokens WHERE id = $1;

-- name: RevokeAPIToken :exec
UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeUserTokens :exec
UPDATE api_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- Groups

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1;

-- name: ListGroups :many
SELECT g.*, (SELECT count(*) FROM group_members gm WHERE gm.group_id = g.id)::int AS member_count
FROM groups g
WHERE @q::text = '' OR g.display_name ILIKE '%' || @q || '%'
ORDER BY g.display_name;

-- name: CreateGroup :one
INSERT INTO groups (display_name, description, source, scim_connection_id, external_id, email, org_role)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateGroup :one
UPDATE groups SET display_name = @display_name, description = @description, external_id = @external_id,
    email = @email, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: SetGroupOrgRole :one
UPDATE groups SET org_role = @org_role, updated_at = now() WHERE id = @id RETURNING *;

-- name: DeleteGroup :exec
DELETE FROM groups WHERE id = $1;

-- name: GroupMembers :many
SELECT u.* FROM users u JOIN group_members gm ON gm.user_id = u.id WHERE gm.group_id = $1 ORDER BY u.display_name;

-- name: GroupMemberIDs :many
SELECT user_id FROM group_members WHERE group_id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: RemoveGroupMember :exec
DELETE FROM group_members WHERE group_id = $1 AND user_id = $2;

-- name: ClearGroupMembers :exec
DELETE FROM group_members WHERE group_id = $1;

-- name: UserGroups :many
SELECT g.* FROM groups g JOIN group_members gm ON gm.group_id = g.id WHERE gm.user_id = $1 ORDER BY g.display_name;

-- name: GetGroupBySourceExternal :one
SELECT * FROM groups
WHERE source = @source AND coalesce(scim_connection_id, '00000000-0000-0000-0000-000000000000'::uuid) = coalesce(sqlc.narg(scim_connection_id)::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND external_id = @external_id;

-- name: ListGroupsBySource :many
SELECT * FROM groups WHERE source = $1;

-- name: UserGroupsBySourcePrefix :many
-- A person's groups from one source whose external id starts with the prefix (one OIDC provider).
SELECT g.* FROM groups g JOIN group_members gm ON gm.group_id = g.id
WHERE gm.user_id = @user_id AND g.source = @source AND g.external_id LIKE @prefix::text || '%';

-- name: ListUsersBySourceWithIdentity :many
-- Users a directory provisioned, with their subject at that directory's sign-in provider.
SELECT u.id, u.active, ui.subject
FROM users u
LEFT JOIN user_identities ui ON ui.user_id = u.id AND ui.provider = @provider
WHERE u.source = @source AND u.deleted_at IS NULL;

-- name: GetUserByIdentity :one
SELECT u.* FROM users u JOIN user_identities ui ON ui.user_id = u.id
WHERE ui.provider = @provider AND ui.subject = @subject;

-- name: GetServiceAccountByUsername :one
SELECT * FROM users WHERE kind = 'service' AND lower(username) = lower(@username::text) AND active AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Roles

-- name: ListRoles :many
SELECT * FROM roles ORDER BY builtin DESC, name;

-- name: GetRole :one
SELECT * FROM roles WHERE id = $1;

-- name: GetRoleByKey :one
SELECT * FROM roles WHERE key = $1;

-- name: CreateRole :one
INSERT INTO roles (key, name, description, permissions) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateRole :one
UPDATE roles SET key = @key, name = @name, description = @description, permissions = @permissions, updated_at = now()
WHERE id = @id RETURNING *;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = $1 AND NOT builtin;

-- ---------------------------------------------------------------------------
-- SCIM connections

-- name: CreateScimConnection :one
INSERT INTO scim_connections (name, token_hash, token_prefix, created_by) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetScimConnectionByToken :one
SELECT * FROM scim_connections WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: TouchScimConnection :exec
UPDATE scim_connections SET last_used_at = now() WHERE id = $1;

-- name: ListScimConnections :many
SELECT c.*,
    (SELECT count(*) FROM users u WHERE u.scim_connection_id = c.id AND u.deleted_at IS NULL)::int AS user_count,
    (SELECT count(*) FROM groups g WHERE g.scim_connection_id = c.id)::int AS group_count
FROM scim_connections c WHERE c.revoked_at IS NULL ORDER BY c.created_at;

-- name: RotateScimConnection :one
UPDATE scim_connections SET token_hash = $2, token_prefix = $3 WHERE id = $1 AND revoked_at IS NULL RETURNING *;

-- name: RevokeScimConnection :exec
UPDATE scim_connections SET revoked_at = now() WHERE id = $1;

-- name: CountActiveScimConnections :one
SELECT count(*) FROM scim_connections WHERE revoked_at IS NULL;

-- ---------------------------------------------------------------------------
-- Directory sync & audit

-- name: CreateDirectorySyncRun :one
INSERT INTO directory_sync_runs (trigger, source) VALUES (@trigger, @source) RETURNING *;

-- name: FinishDirectorySyncRun :exec
UPDATE directory_sync_runs SET status = @status, stats = @stats, error = @error, finished_at = now() WHERE id = @id;

-- name: ListDirectorySyncRuns :many
SELECT * FROM directory_sync_runs ORDER BY started_at DESC LIMIT $1;

-- name: InsertAudit :exec
INSERT INTO audit_events (actor_id, actor_type, action, target_type, target_id, project_id, data, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAudit :many
SELECT * FROM audit_events
WHERE (@action::text = '' OR action LIKE @action || '%')
  AND (sqlc.narg(actor_id)::uuid IS NULL OR actor_id = sqlc.narg(actor_id))
  AND (sqlc.narg(before)::bigint IS NULL OR id < sqlc.narg(before))
ORDER BY id DESC
LIMIT @lim;
