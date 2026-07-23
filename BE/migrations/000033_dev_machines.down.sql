-- This rollback intentionally destroys all Dev Machine control-plane data.
DROP TRIGGER IF EXISTS dev_machine_runtime_credentials_touch ON dev_machine_runtime_credentials;
DROP TRIGGER IF EXISTS dev_machine_operations_touch ON dev_machine_operations;
DROP TRIGGER IF EXISTS dev_machine_checkouts_touch ON dev_machine_checkouts;
DROP TRIGGER IF EXISTS dev_machine_services_touch ON dev_machine_services;
DROP TRIGGER IF EXISTS dev_machine_agent_providers_touch ON dev_machine_agent_providers;
DROP TRIGGER IF EXISTS dev_machine_scope_settings_touch ON dev_machine_scope_settings;
DROP TRIGGER IF EXISTS dev_machines_touch ON dev_machines;
DROP TRIGGER IF EXISTS dev_machine_environments_touch ON dev_machine_environments;
DROP TRIGGER IF EXISTS dev_machine_workspace_policies_touch ON dev_machine_workspace_policies;

ALTER TABLE dev_machine_environments
    DROP CONSTRAINT IF EXISTS fk_dev_machine_environments_source_machine;
ALTER TABLE dev_machines
    DROP CONSTRAINT IF EXISTS fk_dev_machines_environment;

DROP TABLE IF EXISTS dev_machine_access_logs;
DROP TABLE IF EXISTS dev_machine_access_sessions;
DROP TABLE IF EXISTS dev_machine_access_tickets;
DROP TABLE IF EXISTS dev_machine_resource_samples;
DROP TABLE IF EXISTS dev_machine_git_refs;
DROP TABLE IF EXISTS dev_machine_artifacts;
DROP TABLE IF EXISTS dev_machine_log_chunks;
DROP TABLE IF EXISTS dev_machine_events;
DROP TABLE IF EXISTS dev_machine_agent_run_steps;
DROP TABLE IF EXISTS dev_machine_operations;
DROP TABLE IF EXISTS dev_machine_env_vars;
DROP TABLE IF EXISTS dev_machine_tokens;
DROP TABLE IF EXISTS dev_machine_services;
DROP TABLE IF EXISTS dev_machine_agent_runs;
DROP TABLE IF EXISTS dev_machine_terminal_sessions;
DROP TABLE IF EXISTS dev_machine_checkouts;
DROP TABLE IF EXISTS dev_machine_runtime_credentials;
DROP TABLE IF EXISTS dev_machine_volumes;
DROP TABLE IF EXISTS dev_machine_agent_providers;
DROP TABLE IF EXISTS dev_machine_scope_settings;
DROP TABLE IF EXISTS dev_machines;
DROP TABLE IF EXISTS dev_machine_environments;
DROP TABLE IF EXISTS dev_machine_workspace_policies;

DROP FUNCTION IF EXISTS touch_dev_machine_updated_at;

ALTER TABLE github_repos DROP CONSTRAINT IF EXISTS github_repos_workspace_id_id_key;
ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_workspace_id_id_key;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_workspace_id_id_key;
ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_workspace_id_id_key;

DROP TYPE IF EXISTS dev_machine_access_ticket_status;
DROP TYPE IF EXISTS dev_machine_agent_run_step_status;
DROP TYPE IF EXISTS dev_machine_agent_run_status;
DROP TYPE IF EXISTS dev_machine_operation_status;
DROP TYPE IF EXISTS dev_machine_operation_action;
DROP TYPE IF EXISTS dev_machine_status;
