-- Workspace default model and effort for coding agents that run with their own
-- sign-in. Runs fill what they did not choose from it when each run is created.
CREATE TABLE agent_model_defaults (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL CHECK (provider IN ('codex', 'claude-code')),
    model VARCHAR(128) NOT NULL DEFAULT '' CHECK (model = '' OR model ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$'),
    thinking VARCHAR(16) NOT NULL DEFAULT '' CHECK (thinking IN ('', 'off', 'low', 'medium', 'high', 'xhigh', 'max')),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, provider)
);
CREATE TRIGGER agent_model_defaults_touch
    BEFORE UPDATE ON agent_model_defaults
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();

-- Claude Code and Codex both accept medium and xhigh efforts.
ALTER TABLE dev_machine_agent_runs DROP CONSTRAINT dev_machine_agent_runs_thinking_check;
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_thinking_check
    CHECK (thinking IN ('', 'off', 'low', 'medium', 'high', 'xhigh', 'max'));
ALTER TABLE agent_model_servers DROP CONSTRAINT agent_model_servers_thinking_check;
ALTER TABLE agent_model_servers ADD CONSTRAINT agent_model_servers_thinking_check
    CHECK (thinking IN ('off', 'low', 'medium', 'high', 'xhigh', 'max'));
