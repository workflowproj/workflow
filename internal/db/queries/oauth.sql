-- ---------------------------------------------------------------------------
-- OAuth clients (AI assistants)

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE id = $1;

-- name: CreateOAuthClient :one
INSERT INTO oauth_clients (id, kind, name, client_uri, logo_uri, redirect_uris, token_auth_method, secret_hash,
                           software_id, software_version, created_ip)
VALUES (@id, 'dynamic', @name, @client_uri, @logo_uri, @redirect_uris, @token_auth_method, @secret_hash,
        @software_id, @software_version, @created_ip)
RETURNING *;

-- name: UpsertOAuthMetadataClient :one
-- Caches a fetched client ID metadata document.
INSERT INTO oauth_clients (id, kind, name, client_uri, logo_uri, redirect_uris, token_auth_method, software_id,
                           software_version, fetched_at, refresh_after, created_ip)
VALUES (@id, 'metadata', @name, @client_uri, @logo_uri, @redirect_uris, 'none', @software_id, @software_version,
        now(), @refresh_after, @created_ip)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name, client_uri = excluded.client_uri, logo_uri = excluded.logo_uri,
    redirect_uris = excluded.redirect_uris, software_id = excluded.software_id,
    software_version = excluded.software_version, fetched_at = now(), refresh_after = excluded.refresh_after,
    updated_at = now()
WHERE oauth_clients.kind = 'metadata'
RETURNING *;

-- name: DeleteUnusedOAuthClients :execrows
-- Dynamically registered clients nobody ever authorised (abandoned or abusive registrations).
DELETE FROM oauth_clients c
WHERE c.kind = 'dynamic' AND c.created_at < @before
  AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = c.id);

-- ---------------------------------------------------------------------------
-- Grants (connected apps)

-- name: GetActiveOAuthGrant :one
SELECT * FROM oauth_grants WHERE user_id = $1 AND client_id = $2 AND revoked_at IS NULL;

-- name: CreateOAuthGrant :one
INSERT INTO oauth_grants (user_id, client_id, scopes) VALUES ($1, $2, $3) RETURNING *;

-- name: SetOAuthGrantScopes :one
UPDATE oauth_grants SET scopes = @scopes, updated_at = now() WHERE id = @id RETURNING *;

-- name: GetOAuthGrant :one
SELECT sqlc.embed(oauth_grants), sqlc.embed(oauth_clients)
FROM oauth_grants JOIN oauth_clients ON oauth_clients.id = oauth_grants.client_id
WHERE oauth_grants.id = $1;

-- name: ListUserOAuthGrants :many
SELECT sqlc.embed(oauth_grants), sqlc.embed(oauth_clients)
FROM oauth_grants JOIN oauth_clients ON oauth_clients.id = oauth_grants.client_id
WHERE oauth_grants.user_id = $1 AND oauth_grants.revoked_at IS NULL
ORDER BY coalesce(oauth_grants.last_used_at, oauth_grants.created_at) DESC;

-- name: ListOAuthGrants :many
SELECT sqlc.embed(oauth_grants), sqlc.embed(oauth_clients), sqlc.embed(users)
FROM oauth_grants
JOIN oauth_clients ON oauth_clients.id = oauth_grants.client_id
JOIN users ON users.id = oauth_grants.user_id
WHERE oauth_grants.revoked_at IS NULL
ORDER BY coalesce(oauth_grants.last_used_at, oauth_grants.created_at) DESC;

-- name: RevokeOAuthGrant :execrows
UPDATE oauth_grants SET revoked_at = now(), updated_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeOAuthGrantTokens :exec
UPDATE oauth_tokens SET revoked_at = now() WHERE grant_id = $1 AND revoked_at IS NULL;

-- name: TouchOAuthGrant :exec
UPDATE oauth_grants SET last_used_at = now() WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Authorisation codes

-- name: CreateOAuthCode :exec
INSERT INTO oauth_codes (code_hash, grant_id, redirect_uri, code_challenge, resource, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetOAuthCode :one
SELECT sqlc.embed(oauth_codes), sqlc.embed(oauth_grants)
FROM oauth_codes JOIN oauth_grants ON oauth_grants.id = oauth_codes.grant_id
WHERE oauth_codes.code_hash = $1;

-- name: UseOAuthCode :execrows
-- Redeems a code exactly once, recording the token family it produced.
UPDATE oauth_codes SET used_at = now(), family_id = @family_id
WHERE code_hash = @code_hash AND used_at IS NULL AND expires_at > now();

-- ---------------------------------------------------------------------------
-- Tokens

-- name: CreateOAuthToken :one
INSERT INTO oauth_tokens (grant_id, family_id, kind, token_hash, scopes, resource, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetOAuthAccessToken :one
-- A live access token for the given audience, with its grant, client and owner.
SELECT sqlc.embed(oauth_tokens), sqlc.embed(oauth_grants), sqlc.embed(oauth_clients), sqlc.embed(users)
FROM oauth_tokens
JOIN oauth_grants ON oauth_grants.id = oauth_tokens.grant_id
JOIN oauth_clients ON oauth_clients.id = oauth_grants.client_id
JOIN users ON users.id = oauth_grants.user_id
WHERE oauth_tokens.token_hash = @token_hash AND oauth_tokens.kind = 'access' AND oauth_tokens.resource = @resource
  AND oauth_tokens.revoked_at IS NULL AND oauth_tokens.expires_at > now()
  AND oauth_grants.revoked_at IS NULL AND users.active AND users.deleted_at IS NULL;

-- name: GetOAuthToken :one
SELECT sqlc.embed(oauth_tokens), sqlc.embed(oauth_grants)
FROM oauth_tokens JOIN oauth_grants ON oauth_grants.id = oauth_tokens.grant_id
WHERE oauth_tokens.token_hash = $1;

-- name: UseOAuthRefreshToken :execrows
-- Marks a refresh token exchanged (rotation); zero rows means it was already used or is dead.
UPDATE oauth_tokens SET used_at = now()
WHERE id = $1 AND kind = 'refresh' AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now();

-- name: RevokeOAuthFamily :execrows
UPDATE oauth_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeOAuthToken :exec
UPDATE oauth_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: TouchOAuthToken :exec
UPDATE oauth_tokens SET last_used_at = now() WHERE id = $1;

-- name: DeleteExpiredOAuthTokens :execrows
DELETE FROM oauth_tokens WHERE expires_at < @before;

-- name: DeleteExpiredOAuthCodes :execrows
DELETE FROM oauth_codes WHERE expires_at < @before;
