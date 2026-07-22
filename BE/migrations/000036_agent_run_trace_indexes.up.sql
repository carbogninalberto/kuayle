DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT c.conname
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE t.relname = 'dev_machine_log_chunks' AND c.contype = 'u'
    LOOP
        EXECUTE format('ALTER TABLE dev_machine_log_chunks DROP CONSTRAINT %I', constraint_name);
    END LOOP;
END $$;

ALTER TABLE dev_machine_log_chunks
    ADD CONSTRAINT dev_machine_log_chunks_run_sequence_key
    UNIQUE NULLS NOT DISTINCT (machine_id, agent_run_id, service_id, stream, sequence);

CREATE INDEX IF NOT EXISTS idx_dev_machine_events_agent_run_cursor
    ON dev_machine_events(agent_run_id, id)
    WHERE agent_run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dev_machine_log_chunks_agent_run_cursor
    ON dev_machine_log_chunks(agent_run_id, id)
    WHERE agent_run_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS dev_machine_runtime_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    scope VARCHAR(32) NOT NULL DEFAULT 'machine' CHECK (scope = 'machine'),
    credential_type VARCHAR(64) NOT NULL CHECK (credential_type <> ''),
    fingerprint_sha256 VARCHAR(64) NOT NULL CHECK (fingerprint_sha256 ~ '^[a-f0-9]{64}$'),
    encrypted_value TEXT NOT NULL,
    encryption_key_version INT NOT NULL DEFAULT 1,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (machine_id, fingerprint_sha256)
);

CREATE INDEX IF NOT EXISTS idx_dev_machine_runtime_credentials_expiry
    ON dev_machine_runtime_credentials(expires_at);

DROP TRIGGER IF EXISTS dev_machine_runtime_credentials_touch ON dev_machine_runtime_credentials;
CREATE TRIGGER dev_machine_runtime_credentials_touch BEFORE UPDATE ON dev_machine_runtime_credentials
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();
