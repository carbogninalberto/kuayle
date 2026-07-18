ALTER TABLE dev_machine_environments
    ADD COLUMN IF NOT EXISTS delete_requested_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_dev_machine_environments_delete_requested
    ON dev_machine_environments(delete_requested_at, updated_at)
    WHERE status = 'delete_requested';
