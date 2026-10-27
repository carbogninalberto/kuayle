-- Workspace model servers for coding agents. A server speaks the OpenAI
-- protocol (used by Codex), the Anthropic protocol (used by Claude Code), or
-- both. Its API key is encrypted at rest and never returned by the API.
CREATE TABLE agent_model_servers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 64),
    openai_base_url TEXT CHECK (openai_base_url IS NULL OR (openai_base_url ~ '^https?://[^[:space:]]+$' AND char_length(openai_base_url) <= 512)),
    anthropic_base_url TEXT CHECK (anthropic_base_url IS NULL OR (anthropic_base_url ~ '^https?://[^[:space:]]+$' AND char_length(anthropic_base_url) <= 512)),
    encrypted_api_key TEXT NOT NULL,
    encryption_key_version INT NOT NULL DEFAULT 1,
    models JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(models) = 'array' AND jsonb_array_length(models) <= 500),
    default_model VARCHAR(128) NOT NULL CHECK (default_model ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$'),
    fast_model VARCHAR(128) NOT NULL DEFAULT '' CHECK (fast_model = '' OR fast_model ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$'),
    thinking VARCHAR(16) NOT NULL DEFAULT 'high' CHECK (thinking IN ('off', 'low', 'high', 'max')),
    is_default BOOLEAN NOT NULL DEFAULT false,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (openai_base_url IS NOT NULL OR anthropic_base_url IS NOT NULL),
    UNIQUE (workspace_id, name)
);
CREATE UNIQUE INDEX agent_model_servers_one_default ON agent_model_servers(workspace_id) WHERE is_default;
CREATE TRIGGER agent_model_servers_touch
    BEFORE UPDATE ON agent_model_servers
    FOR EACH ROW EXECUTE FUNCTION touch_dev_machine_updated_at();

-- The workspace DeepSeek credential becomes the default server, unchanged.
INSERT INTO agent_model_servers (workspace_id, name, openai_base_url, anthropic_base_url, encrypted_api_key, encryption_key_version, models, default_model, fast_model, thinking, is_default, updated_by, created_at, updated_at)
SELECT workspace_id, 'DeepSeek', 'https://api.deepseek.com', 'https://api.deepseek.com/anthropic', encrypted_api_key, encryption_key_version,
       '["deepseek-flash","deepseek-v4-pro"]'::jsonb, default_model, 'deepseek-flash', thinking, true, updated_by, created_at, updated_at
FROM dev_machine_workspace_model_credentials WHERE provider = 'deepseek';
DROP TABLE dev_machine_workspace_model_credentials;

-- Any model a server offers may be selected; the server is recorded per run.
ALTER TABLE dev_machine_agent_runs DROP CONSTRAINT dev_machine_agent_runs_model_check;
ALTER TABLE dev_machine_agent_runs ALTER COLUMN model TYPE VARCHAR(128);
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_model_check
    CHECK (model = '' OR model ~ '^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$');
ALTER TABLE dev_machine_agent_runs ADD COLUMN model_server_id UUID;
ALTER TABLE agent_tasks ADD COLUMN model_server_id UUID REFERENCES agent_model_servers(id) ON DELETE SET NULL;
