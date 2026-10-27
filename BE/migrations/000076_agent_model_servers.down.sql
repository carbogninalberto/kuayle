ALTER TABLE agent_tasks DROP COLUMN model_server_id;
ALTER TABLE dev_machine_agent_runs DROP COLUMN model_server_id;
ALTER TABLE dev_machine_agent_runs DROP CONSTRAINT dev_machine_agent_runs_model_check;
UPDATE dev_machine_agent_runs SET model = '' WHERE model NOT IN ('', 'deepseek-flash', 'deepseek-v4-pro');
ALTER TABLE dev_machine_agent_runs ALTER COLUMN model TYPE VARCHAR(64);
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_model_check
    CHECK (model IN ('', 'deepseek-flash', 'deepseek-v4-pro'));

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
INSERT INTO dev_machine_workspace_model_credentials (workspace_id, provider, encrypted_api_key, encryption_key_version, default_model, thinking, updated_by, created_at, updated_at)
SELECT workspace_id, 'deepseek', encrypted_api_key, encryption_key_version, default_model, thinking, updated_by, created_at, updated_at
FROM agent_model_servers WHERE name = 'DeepSeek' AND default_model IN ('deepseek-flash', 'deepseek-v4-pro');
DROP TABLE agent_model_servers;
