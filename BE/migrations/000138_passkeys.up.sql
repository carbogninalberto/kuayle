-- WebAuthn passkeys. One row per credential a user registered.
CREATE TABLE passkey_credentials (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,
    public_key BYTEA NOT NULL,
    sign_count BIGINT NOT NULL DEFAULT 0,
    transports TEXT[] NOT NULL DEFAULT '{}',
    aaguid BYTEA NOT NULL DEFAULT ''::bytea,
    attestation_type TEXT NOT NULL DEFAULT '',
    attestation_format TEXT NOT NULL DEFAULT '',
    user_verified BOOLEAN NOT NULL DEFAULT FALSE,
    backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state BOOLEAN NOT NULL DEFAULT FALSE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);
CREATE INDEX idx_passkey_credentials_user ON passkey_credentials(user_id);

-- Pending ceremonies. A row is deleted when its ceremony completes or fails, so
-- a challenge is single-use; only the SHA-256 hash of the browser's handle is stored.
CREATE TABLE passkey_challenges (
    handle_hash TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('registration', 'login')),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    session_data JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((kind = 'registration') = (user_id IS NOT NULL))
);
CREATE INDEX idx_passkey_challenges_user ON passkey_challenges(user_id);
CREATE INDEX idx_passkey_challenges_expires ON passkey_challenges(expires_at);

-- A session remembers when its user last authenticated and, for passkey
-- sign-ins, which credential minted it: deleting the credential deletes
-- exactly those sessions. Existing sessions keep NULL in both columns.
ALTER TABLE refresh_tokens
    ADD COLUMN authenticated_at TIMESTAMPTZ,
    ADD COLUMN passkey_credential_id UUID REFERENCES passkey_credentials(id) ON DELETE CASCADE;
CREATE INDEX idx_refresh_tokens_passkey ON refresh_tokens(passkey_credential_id) WHERE passkey_credential_id IS NOT NULL;
