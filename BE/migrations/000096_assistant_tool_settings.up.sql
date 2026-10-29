-- Per-workspace switches for the assistant's built-in tools. No row means the
-- tool is enabled; owners and admins turn tools off (and on again) in
-- Settings → Assistant. A disabled tool is neither offered to the model nor
-- run, and a pending approval for it fails when approved.
CREATE TABLE assistant_tool_settings (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL CHECK (tool_name ~ '^[a-z][a-z0-9_]{0,63}$'),
    enabled BOOLEAN NOT NULL,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, tool_name)
);
