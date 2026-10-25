ALTER TABLE dev_machine_agent_runs
    DROP COLUMN IF EXISTS thinking,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS command_environment;

DROP TABLE IF EXISTS dev_machine_workspace_model_credentials;
