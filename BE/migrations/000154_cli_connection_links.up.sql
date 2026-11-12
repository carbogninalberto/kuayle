-- KUA-111: connection links let kuayle_cli configure itself from a link
-- instead of a manually copied token. A link holds only the SHA-256 of its
-- code (the plaintext is shown once), a display prefix, the scopes and
-- workspace restriction for the token it mints, its own TTL and use budget,
-- and a revocation timestamp. Tokens minted by a link keep working after the
-- link is revoked or expired; a password reset revokes open links together
-- with tokens.
CREATE TABLE cli_connection_links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    code_hash        TEXT NOT NULL UNIQUE,
    code_prefix      TEXT NOT NULL,
    scopes           TEXT[] NOT NULL DEFAULT '{}',
    workspace_slugs  TEXT[],
    token_expires_at TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ NOT NULL,
    max_uses         INTEGER NOT NULL DEFAULT 1 CHECK (max_uses BETWEEN 1 AND 10),
    uses             INTEGER NOT NULL DEFAULT 0 CHECK (uses >= 0 AND uses <= max_uses),
    revoked_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cli_connection_links_user ON cli_connection_links(user_id);

ALTER TABLE personal_access_tokens
    ADD COLUMN connection_link_id UUID REFERENCES cli_connection_links(id) ON DELETE SET NULL;
