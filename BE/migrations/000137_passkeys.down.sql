-- Sessions minted with a passkey end with it; password sessions are kept.
DELETE FROM refresh_tokens WHERE passkey_credential_id IS NOT NULL;
DROP INDEX IF EXISTS idx_refresh_tokens_passkey;
ALTER TABLE refresh_tokens
    DROP COLUMN IF EXISTS passkey_credential_id,
    DROP COLUMN IF EXISTS authenticated_at;
DROP TABLE IF EXISTS passkey_challenges;
DROP TABLE IF EXISTS passkey_credentials;
