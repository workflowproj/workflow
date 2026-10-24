-- Local accounts: passwords, two-factor, invitation and reset links, sign-in challenges.

-- name: GetLocalCredentials :one
SELECT * FROM local_credentials WHERE user_id = $1;

-- name: EnsureLocalCredentials :one
-- Makes the person a local account (without a password yet, e.g. when invited).
INSERT INTO local_credentials (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET updated_at = now()
RETURNING *;

-- name: SetLocalPassword :one
-- A new password (chosen, reset or temporary) also clears any lockout.
INSERT INTO local_credentials (user_id, password_hash, must_change, password_changed_at)
VALUES (@user_id, @password_hash, @must_change, now())
ON CONFLICT (user_id) DO UPDATE SET
    password_hash = excluded.password_hash,
    must_change = excluded.must_change,
    password_changed_at = now(),
    failed_attempts = 0, last_failed_at = NULL, locked_until = NULL,
    updated_at = now()
RETURNING *;

-- name: RehashLocalPassword :exec
-- Same password, current hashing parameters.
UPDATE local_credentials SET password_hash = @password_hash, updated_at = now() WHERE user_id = @user_id;

-- name: RecordLoginFailure :one
-- Counts a failed sign-in. Failures older than the lockout window start the count again;
-- reaching the threshold locks the account until @locked_until and restarts the count.
UPDATE local_credentials SET
    failed_attempts = CASE
        WHEN last_failed_at IS NULL OR last_failed_at < @window_start::timestamptz THEN 1
        ELSE failed_attempts + 1 END,
    last_failed_at = now(),
    updated_at = now()
WHERE user_id = @user_id
RETURNING failed_attempts;

-- name: LockLocalAccount :exec
UPDATE local_credentials SET locked_until = @locked_until, failed_attempts = 0, updated_at = now() WHERE user_id = @user_id;

-- name: ClearLoginFailures :exec
UPDATE local_credentials SET failed_attempts = 0, last_failed_at = NULL, locked_until = NULL, updated_at = now()
WHERE user_id = $1 AND (failed_attempts <> 0 OR locked_until IS NOT NULL);

-- name: SetTOTPPending :exec
UPDATE local_credentials SET totp_pending = @totp_pending, updated_at = now() WHERE user_id = @user_id;

-- name: EnableTOTP :exec
UPDATE local_credentials SET totp_secret = @totp_secret, totp_pending = NULL, totp_enabled_at = now(),
    totp_last_step = @step, updated_at = now()
WHERE user_id = @user_id;

-- name: UseTOTPStep :execrows
-- Accepts a code's time step once: a step at or before the last one used is a replay.
UPDATE local_credentials SET totp_last_step = @step, updated_at = now()
WHERE user_id = @user_id AND (totp_last_step IS NULL OR totp_last_step < @step);

-- name: DisableTOTP :exec
UPDATE local_credentials SET totp_secret = NULL, totp_enabled_at = NULL, totp_last_step = NULL, totp_pending = NULL, updated_at = now()
WHERE user_id = $1;

-- name: DeleteLocalCredentials :exec
DELETE FROM local_credentials WHERE user_id = $1;

-- name: ListLocalCredentials :many
SELECT * FROM local_credentials WHERE user_id = ANY(@ids::uuid[]);

-- name: OpenInvitations :many
-- The latest unused invitation of each person.
SELECT DISTINCT ON (user_id) user_id, expires_at FROM account_tokens
WHERE user_id = ANY(@ids::uuid[]) AND purpose = 'invite' AND used_at IS NULL
ORDER BY user_id, created_at DESC;

-- ---------------------------------------------------------------------------
-- Recovery codes

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: InsertRecoveryCodes :exec
INSERT INTO recovery_codes (user_id, code_hash) SELECT @user_id, unnest(@code_hashes::text[]);

-- name: UseRecoveryCode :execrows
UPDATE recovery_codes SET used_at = now() WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: CountRecoveryCodes :one
SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- ---------------------------------------------------------------------------
-- Invitation and password-reset links

-- name: CreateAccountToken :one
INSERT INTO account_tokens (user_id, purpose, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetOpenAccountToken :one
SELECT * FROM account_tokens WHERE token_hash = @token_hash AND purpose = @purpose AND used_at IS NULL AND expires_at > now();

-- name: UseAccountToken :execrows
UPDATE account_tokens SET used_at = now() WHERE id = $1 AND used_at IS NULL AND expires_at > now();

-- name: CloseAccountTokens :exec
-- Revokes a person's outstanding links of one purpose (or every purpose when empty).
UPDATE account_tokens SET used_at = now()
WHERE user_id = @user_id AND used_at IS NULL AND (@purpose::text = '' OR purpose = @purpose);

-- name: PruneAccountTokens :execrows
DELETE FROM account_tokens WHERE coalesce(used_at, expires_at) < @before::timestamptz;

-- ---------------------------------------------------------------------------
-- Sign-in challenges

-- name: CreateLoginChallenge :exec
INSERT INTO login_challenges (id, user_id, stage, expires_at) VALUES ($1, $2, $3, $4);

-- name: GetLoginChallenge :one
SELECT * FROM login_challenges WHERE id = $1 AND expires_at > now();

-- name: SetLoginChallengeStage :exec
UPDATE login_challenges SET stage = @stage, attempts = 0 WHERE id = @id;

-- name: CountLoginChallengeAttempt :one
UPDATE login_challenges SET attempts = attempts + 1 WHERE id = $1 RETURNING attempts;

-- name: DeleteLoginChallenge :exec
DELETE FROM login_challenges WHERE id = $1;

-- name: DeleteUserLoginChallenges :exec
DELETE FROM login_challenges WHERE user_id = $1;

-- name: DeleteExpiredLoginChallenges :execrows
DELETE FROM login_challenges WHERE expires_at < now();

-- name: DeleteUserSessionsExcept :exec
-- Ends a person's other sessions (after a password change), keeping the current one.
DELETE FROM sessions WHERE user_id = @user_id AND id <> @keep_id;
