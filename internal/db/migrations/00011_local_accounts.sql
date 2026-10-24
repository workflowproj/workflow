-- +goose Up
-- Local accounts: people who sign in with an email address and a password (contractors, people
-- outside the identity provider, small teams without SSO). A person is a local account when
-- they have a row here; single sign-on keeps working for them too when it matches their email.
CREATE TABLE local_credentials (
    user_id             uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- argon2id in PHC string format; NULL until an invited person chooses a password.
    password_hash       text,
    -- Set with a temporary password: the person must choose a new one at their next sign-in.
    must_change         boolean NOT NULL DEFAULT false,
    password_changed_at timestamptz,
    -- Consecutive failed sign-ins (password or second factor); reaching the threshold locks
    -- the account until locked_until.
    failed_attempts     int NOT NULL DEFAULT 0,
    last_failed_at      timestamptz,
    locked_until        timestamptz,
    -- TOTP (RFC 6238) secrets sealed with WFM_SECRET_KEY. totp_pending holds a secret being set
    -- up until a code confirms it; totp_last_step stops a code being used twice.
    totp_secret         text,
    totp_enabled_at     timestamptz,
    totp_last_step      bigint,
    totp_pending        text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- One-time recovery codes for a lost authenticator, stored as sha256.
CREATE TABLE recovery_codes (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  text NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, code_hash)
);

-- Single-use links sent by email: invitations (7 days) and password resets (1 hour). Only the
-- sha256 of the random part is stored; the link also carries a signature.
CREATE TABLE account_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('invite', 'reset')),
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX account_tokens_open_idx ON account_tokens (user_id, purpose) WHERE used_at IS NULL;

-- The unfinished second step of a password sign-in: a two-factor code, a required password
-- change or two-factor enrolment. The browser holds the token in an HttpOnly cookie.
CREATE TABLE login_challenges (
    -- sha256 of the cookie token.
    id         text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    stage      text NOT NULL CHECK (stage IN ('totp', 'password_change', 'totp_enroll')),
    attempts   int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_challenges_user_idx ON login_challenges (user_id);
CREATE INDEX login_challenges_expires_idx ON login_challenges (expires_at);

-- When SCIM or Google directory sync takes a person over (matching their email), the identity
-- provider owns their sign-in from then on: their password, two-factor and links are removed.
-- +goose StatementBegin
CREATE FUNCTION users_drop_local_credentials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM local_credentials WHERE user_id = NEW.id;
    DELETE FROM recovery_codes WHERE user_id = NEW.id;
    DELETE FROM account_tokens WHERE user_id = NEW.id;
    DELETE FROM login_challenges WHERE user_id = NEW.id;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER users_drop_local_credentials AFTER UPDATE OF source ON users
    FOR EACH ROW WHEN (NEW.source IN ('scim', 'google') AND OLD.source IS DISTINCT FROM NEW.source)
    EXECUTE FUNCTION users_drop_local_credentials();

-- +goose Down
DROP TRIGGER users_drop_local_credentials ON users;
DROP FUNCTION users_drop_local_credentials();
DROP TABLE login_challenges, account_tokens, recovery_codes, local_credentials;
