-- Personal access tokens (PAT) for non-interactive API authentication (CLI/CI/agents).
-- Plaintext tokens are never stored; only the SHA-256 hex of the token is kept,
-- same convention as refresh_tokens.
CREATE TABLE personal_access_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    token_prefix    TEXT NOT NULL,
    scopes          TEXT[] NOT NULL DEFAULT '{}',
    workspace_slugs TEXT[],
    expires_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX idx_pat_user ON personal_access_tokens(user_id);
