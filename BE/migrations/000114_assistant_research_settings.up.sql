-- Research settings of the assistant (KUA-93): the web search provider and its
-- API key (encrypted by the server, never returned to clients), and the model
-- and parallelism of research sub-tasks. No row means the defaults: Brave,
-- no key (web_search reports it is not configured), the conversation's own
-- model and three sub-tasks at once. Only owners and admins write it.
CREATE TABLE assistant_research_settings (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    search_provider TEXT NOT NULL DEFAULT 'brave' CHECK (search_provider IN ('brave', 'tavily')),
    search_api_key_encrypted TEXT CHECK (search_api_key_encrypted IS NULL OR char_length(search_api_key_encrypted) BETWEEN 1 AND 4096),
    subagent_model TEXT NOT NULL DEFAULT '' CHECK (char_length(subagent_model) <= 200),
    subagent_max_parallel SMALLINT NOT NULL DEFAULT 3 CHECK (subagent_max_parallel BETWEEN 1 AND 8),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
