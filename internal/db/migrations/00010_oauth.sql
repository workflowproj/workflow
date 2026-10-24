-- +goose Up
-- OAuth 2.1 sign-in for AI assistants (MCP clients). Workflow is its own authorisation server:
-- an assistant registers (or identifies itself with a client ID metadata document), the person
-- approves access in the browser, and the assistant receives short-lived access tokens for /mcp.

-- Clients. Dynamically registered clients (RFC 7591) get a random id; clients identified by a
-- client ID metadata document use the document's https URL as their id, and the fetched document
-- is cached here until refresh_after.
CREATE TABLE oauth_clients (
    id                  text PRIMARY KEY,
    kind                text NOT NULL CHECK (kind IN ('dynamic', 'metadata')),
    name                text NOT NULL,
    client_uri          text,
    logo_uri            text,
    redirect_uris       text[] NOT NULL,
    -- none (public client with PKCE), client_secret_post or client_secret_basic
    token_auth_method   text NOT NULL DEFAULT 'none',
    -- sha256 of the client secret, for confidential dynamically registered clients
    secret_hash         text,
    software_id         text,
    software_version    text,
    fetched_at          timestamptz,
    refresh_after       timestamptz,
    created_ip          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX oauth_clients_created_idx ON oauth_clients (created_at);

-- A person's authorisation of a client: a "connected app". Consent is remembered while an
-- active grant covers the scopes a client asks for. Revoking a grant revokes its tokens.
CREATE TABLE oauth_grants (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_id    text NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    scopes       text[] NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at   timestamptz
);
CREATE UNIQUE INDEX oauth_grants_active_key ON oauth_grants (user_id, client_id) WHERE revoked_at IS NULL;
CREATE INDEX oauth_grants_client_idx ON oauth_grants (client_id);

-- Authorisation codes (stored hashed): single use, short-lived, bound to the PKCE challenge,
-- the redirect URI and the resource. family_id records the tokens issued for the code, so a
-- replayed code revokes them.
CREATE TABLE oauth_codes (
    code_hash      text PRIMARY KEY,
    grant_id       uuid NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    resource       text NOT NULL,
    scopes         text[] NOT NULL,
    family_id      uuid,
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX oauth_codes_expires_idx ON oauth_codes (expires_at);

-- Access and refresh tokens (stored hashed). A family is the chain of refresh tokens from one
-- authorisation; presenting a refresh token that was already exchanged revokes the family.
CREATE TABLE oauth_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    grant_id     uuid NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    family_id    uuid NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('access', 'refresh')),
    token_hash   text NOT NULL UNIQUE,
    scopes       text[] NOT NULL,
    -- the audience (RFC 8707 resource): the MCP endpoint
    resource     text NOT NULL,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    -- refresh tokens: when it was exchanged for a new pair
    used_at      timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX oauth_tokens_family_idx ON oauth_tokens (family_id);
CREATE INDEX oauth_tokens_grant_idx ON oauth_tokens (grant_id);
CREATE INDEX oauth_tokens_expires_idx ON oauth_tokens (expires_at);

-- +goose Down
DROP TABLE oauth_tokens, oauth_codes, oauth_grants, oauth_clients;
