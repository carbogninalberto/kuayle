DROP TRIGGER IF EXISTS dev_machine_checkouts_touch ON dev_machine_checkouts;
DROP TRIGGER IF EXISTS dev_machine_scope_settings_touch ON dev_machine_scope_settings;
DROP TRIGGER IF EXISTS dev_machine_environments_touch ON dev_machine_environments;

ALTER TABLE dev_machine_services
    DROP CONSTRAINT IF EXISTS dev_machine_services_service_type_check;
DELETE FROM dev_machine_services WHERE service_type = 'terminal';
ALTER TABLE dev_machine_services
    ADD CONSTRAINT dev_machine_services_service_type_check CHECK (
        service_type IN ('ide', 'agent', 'browser', 'collector', 'egress')
    );

ALTER TABLE dev_machine_access_tickets DROP COLUMN IF EXISTS terminal_session_id;
ALTER TABLE dev_machine_operations
    DROP COLUMN IF EXISTS terminal_session_id,
    DROP COLUMN IF EXISTS checkout_id,
    DROP COLUMN IF EXISTS environment_id;
ALTER TABLE dev_machine_agent_runs DROP COLUMN IF EXISTS checkout_id;
DROP TABLE IF EXISTS dev_machine_terminal_sessions;
DROP TABLE IF EXISTS dev_machine_checkouts;

DROP INDEX IF EXISTS idx_dev_machines_delete_requested;
DROP INDEX IF EXISTS idx_dev_machines_idle;
DROP INDEX IF EXISTS idx_dev_machines_workspace_name;

ALTER TABLE dev_machines
    DROP COLUMN IF EXISTS delete_requested_at,
    DROP COLUMN IF EXISTS environment_builder,
    DROP COLUMN IF EXISTS keep_running,
    DROP COLUMN IF EXISTS repository_affinity_id,
    DROP COLUMN IF EXISTS environment_id;

ALTER TABLE dev_machines
    ALTER COLUMN repo_url DROP DEFAULT,
    ALTER COLUMN repo_owner DROP DEFAULT,
    ALTER COLUMN repo_name DROP DEFAULT,
    ALTER COLUMN working_branch DROP DEFAULT,
    ALTER COLUMN base_branch SET DEFAULT 'main';

DROP TABLE IF EXISTS dev_machine_scope_settings;
DROP TABLE IF EXISTS dev_machine_environments;
ALTER TABLE dev_machine_workspace_policies DROP COLUMN IF EXISTS idle_pause_minutes;

-- PostgreSQL enum values are intentionally retained on rollback.
