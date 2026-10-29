-- Workspace custom tools for the assistant: a script with a strict parameter
-- schema that runs on a dev machine in the credential-free check container.
-- The tool row holds the content in effect (its current_version); every saved
-- edit is a row in assistant_custom_tool_versions. A draft version above
-- current_version is a proposed edit an owner or admin has not activated yet;
-- the assistant only ever writes drafts. A tool runs without approval (auto)
-- only when an owner or admin marked it read-only.
CREATE TABLE assistant_custom_tools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (name ~ '^[a-z][a-z0-9_]{0,47}$'),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 1000),
    parameters JSONB NOT NULL CHECK (jsonb_typeof(parameters) = 'object' AND octet_length(parameters::text) <= 16384),
    script TEXT NOT NULL CHECK (octet_length(script) BETWEEN 1 AND 65536),
    interpreter TEXT NOT NULL CHECK (interpreter IN ('sh', 'bash', 'node', 'python3')),
    run_mode TEXT NOT NULL CHECK (run_mode IN ('approval', 'auto')),
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    timeout_seconds INT NOT NULL DEFAULT 600 CHECK (timeout_seconds BETWEEN 10 AND 1800),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL CHECK (status IN ('draft', 'active')),
    current_version INT NOT NULL CHECK (current_version >= 1),
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    author_kind TEXT NOT NULL CHECK (author_kind IN ('user', 'assistant')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (run_mode = 'approval' OR read_only),
    UNIQUE (workspace_id, id)
);
CREATE UNIQUE INDEX assistant_custom_tools_name ON assistant_custom_tools (workspace_id, name);

CREATE TABLE assistant_custom_tool_versions (
    tool_id UUID NOT NULL REFERENCES assistant_custom_tools(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version >= 1),
    name TEXT NOT NULL CHECK (name ~ '^[a-z][a-z0-9_]{0,47}$'),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 1000),
    parameters JSONB NOT NULL CHECK (jsonb_typeof(parameters) = 'object' AND octet_length(parameters::text) <= 16384),
    script TEXT NOT NULL CHECK (octet_length(script) BETWEEN 1 AND 65536),
    interpreter TEXT NOT NULL CHECK (interpreter IN ('sh', 'bash', 'node', 'python3')),
    run_mode TEXT NOT NULL CHECK (run_mode IN ('approval', 'auto')),
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    timeout_seconds INT NOT NULL DEFAULT 600 CHECK (timeout_seconds BETWEEN 10 AND 1800),
    -- draft: proposed and not activated yet; active: has been put in effect.
    status TEXT NOT NULL CHECK (status IN ('draft', 'active')),
    author_kind TEXT NOT NULL CHECK (author_kind IN ('user', 'assistant')),
    author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (run_mode = 'approval' OR read_only),
    PRIMARY KEY (tool_id, version)
);
