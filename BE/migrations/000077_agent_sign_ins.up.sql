-- A coding agent's own sign-in (Claude Code or Codex subscription login),
-- captured from the user's terminal and kept encrypted per user. It is used
-- only for that user's runs when no model server serves the agent.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'capture_sign_in';
CREATE TABLE agent_sign_ins (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL CHECK (provider IN ('codex', 'claude-code')),
    encrypted_credential TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, user_id, provider)
);
-- The task stopped because its agent needs the user to sign in first.
ALTER TABLE agent_tasks ADD COLUMN sign_in_required BOOLEAN NOT NULL DEFAULT false;
