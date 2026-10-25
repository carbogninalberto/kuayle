CREATE TABLE dev_machine_workspace_model_credentials (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL CHECK (provider IN ('deepseek')),
    encrypted_api_key TEXT NOT NULL,
    encryption_key_version INT NOT NULL DEFAULT 1,
    default_model VARCHAR(64) NOT NULL DEFAULT 'deepseek-flash',
    thinking VARCHAR(16) NOT NULL DEFAULT 'high' CHECK (thinking IN ('off', 'low', 'high', 'max')),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, provider)
);

CREATE TRIGGER dev_machine_workspace_model_credentials_touch
    BEFORE UPDATE ON dev_machine_workspace_model_credentials
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();

ALTER TABLE dev_machine_agent_runs
    ADD COLUMN command_environment JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN model VARCHAR(64) NOT NULL DEFAULT ''
        CHECK (model IN ('', 'deepseek-flash', 'deepseek-v4-pro')),
    ADD COLUMN thinking VARCHAR(16) NOT NULL DEFAULT ''
        CHECK (thinking IN ('', 'off', 'low', 'high', 'max'));
