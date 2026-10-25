-- +goose Up
-- Keycloak directory sync, groups from OIDC token claims, and the IdP session details needed
-- for back-channel and RP-initiated logout.

ALTER TABLE users DROP CONSTRAINT users_source_check,
    ADD CONSTRAINT users_source_check CHECK (source IN ('jit', 'scim', 'google', 'keycloak', 'admin', 'dev', 'system'));

-- 'keycloak': synced from a Keycloak realm. 'oidc': taken from a sign-in token's groups claim;
-- external_id is "<provider>|<group>" so each provider's groups stay apart.
ALTER TABLE groups DROP CONSTRAINT groups_source_check,
    ADD CONSTRAINT groups_source_check CHECK (source IN ('local', 'scim', 'google', 'keycloak', 'oidc'));

ALTER TABLE directory_sync_runs
    ADD COLUMN source text NOT NULL DEFAULT 'google' CHECK (source IN ('google', 'keycloak'));

-- Which IdP session a Workflow session came from. idp_sid / idp_subject let the IdP end it
-- (back-channel logout); idp_id_token (sealed) is the hint for RP-initiated logout.
ALTER TABLE sessions
    ADD COLUMN idp_provider text,
    ADD COLUMN idp_subject  text,
    ADD COLUMN idp_sid      text,
    ADD COLUMN idp_id_token text;
CREATE INDEX sessions_idp_sid_idx ON sessions (idp_provider, idp_sid) WHERE idp_sid IS NOT NULL;
CREATE INDEX sessions_idp_subject_idx ON sessions (idp_provider, idp_subject) WHERE idp_provider IS NOT NULL;

-- A Keycloak takeover removes local credentials, like SCIM and Google directory sync.
DROP TRIGGER users_drop_local_credentials ON users;
CREATE TRIGGER users_drop_local_credentials AFTER UPDATE OF source ON users
    FOR EACH ROW WHEN (NEW.source IN ('scim', 'google', 'keycloak') AND OLD.source IS DISTINCT FROM NEW.source)
    EXECUTE FUNCTION users_drop_local_credentials();

-- +goose Down
DROP TRIGGER users_drop_local_credentials ON users;
CREATE TRIGGER users_drop_local_credentials AFTER UPDATE OF source ON users
    FOR EACH ROW WHEN (NEW.source IN ('scim', 'google') AND OLD.source IS DISTINCT FROM NEW.source)
    EXECUTE FUNCTION users_drop_local_credentials();
DROP INDEX sessions_idp_subject_idx, sessions_idp_sid_idx;
ALTER TABLE sessions DROP COLUMN idp_id_token, DROP COLUMN idp_sid, DROP COLUMN idp_subject, DROP COLUMN idp_provider;
DELETE FROM directory_sync_runs WHERE source = 'keycloak';
ALTER TABLE directory_sync_runs DROP COLUMN source;
DELETE FROM groups WHERE source IN ('keycloak', 'oidc');
ALTER TABLE groups DROP CONSTRAINT groups_source_check,
    ADD CONSTRAINT groups_source_check CHECK (source IN ('local', 'scim', 'google'));
UPDATE users SET source = 'jit' WHERE source = 'keycloak';
ALTER TABLE users DROP CONSTRAINT users_source_check,
    ADD CONSTRAINT users_source_check CHECK (source IN ('jit', 'scim', 'google', 'admin', 'dev', 'system'));
