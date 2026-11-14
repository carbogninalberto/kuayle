-- OIDC single sign-on (KUA-99 phase 1). One configuration per workspace, the
-- external identities that signed in through it, and the short-lived server
-- side state of an authorization-code flow.

CREATE TABLE workspace_sso_configs (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    issuer_url TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_encrypted TEXT NOT NULL DEFAULT '',
    allowed_domains TEXT[] NOT NULL DEFAULT '{}',
    provider_name TEXT NOT NULL DEFAULT '',
    default_role TEXT NOT NULL DEFAULT 'member' CHECK (default_role IN ('member', 'guest')),
    require_sso BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The users table stays unchanged so existing email/password accounts keep
-- working; an external identity is a separate row per workspace.
CREATE TABLE user_identities (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ,
    UNIQUE (workspace_id, issuer, subject)
);

CREATE INDEX idx_user_identities_user ON user_identities(user_id);

-- Only the SHA-256 hash of the OAuth state and of the browser binding value is
-- stored. The PKCE verifier is encrypted with the instance key.
CREATE TABLE sso_login_states (
    state_hash TEXT PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    nonce TEXT NOT NULL,
    code_verifier_encrypted TEXT NOT NULL,
    bind_hash TEXT NOT NULL,
    redirect_after TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sso_login_states_expires_at ON sso_login_states(expires_at);

-- A session minted by workspace SSO keeps that workspace across refresh token
-- rotation, so require-SSO enforcement survives an access token refresh.
ALTER TABLE refresh_tokens ADD COLUMN sso_workspace_id UUID NULL REFERENCES workspaces(id) ON DELETE CASCADE;
