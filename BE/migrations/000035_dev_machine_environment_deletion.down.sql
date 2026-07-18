DROP INDEX IF EXISTS idx_dev_machine_environments_delete_requested;

ALTER TABLE dev_machine_environments
    DROP COLUMN IF EXISTS delete_requested_at;
