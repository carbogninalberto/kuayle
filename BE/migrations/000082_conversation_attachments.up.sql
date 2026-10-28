-- Images attached to assistant messages. An upload starts pending (no
-- message) and is bound to exactly one user message when that message is
-- sent; only the conversation's authorized readers can read it. Objects live
-- in the configured storage backend under storage_key.
CREATE TABLE conversation_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_id UUID REFERENCES conversation_messages(id) ON DELETE CASCADE,
    uploaded_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    storage_key TEXT NOT NULL UNIQUE,
    filename TEXT NOT NULL CHECK (char_length(filename) BETWEEN 1 AND 255),
    content_type TEXT NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp', 'image/gif')),
    size BIGINT NOT NULL CHECK (size > 0 AND size <= 10485760),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    position SMALLINT NOT NULL DEFAULT 0 CHECK (position >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_conversation_attachments_message ON conversation_attachments(message_id, position) WHERE message_id IS NOT NULL;
CREATE INDEX idx_conversation_attachments_pending ON conversation_attachments(conversation_id, uploaded_by, created_at) WHERE message_id IS NULL;

-- A conversation image filed on an issue is copied into an issue asset, so
-- issue readers can see it through the existing asset rules.
CREATE TABLE conversation_attachment_issues (
    attachment_id UUID NOT NULL REFERENCES conversation_attachments(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (attachment_id, issue_id)
);

-- Deleted attachment rows (including cascades from conversations, users or
-- workspaces) leave their storage key here until the API removes the object.
CREATE TABLE conversation_attachment_garbage (
    storage_key TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE FUNCTION kuayle_conversation_attachment_garbage() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO conversation_attachment_garbage(storage_key) VALUES (OLD.storage_key) ON CONFLICT DO NOTHING;
    RETURN OLD;
END;
$$;
CREATE TRIGGER conversation_attachment_garbage AFTER DELETE ON conversation_attachments
    FOR EACH ROW EXECUTE FUNCTION kuayle_conversation_attachment_garbage();

-- Images a coding task attempt receives, fixed when the attempt is created so
-- the prompt and the files copied into the worktree always match.
CREATE TABLE agent_task_attachments (
    task_id UUID NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
    run_id UUID NOT NULL,
    position SMALLINT NOT NULL CHECK (position >= 0 AND position < 10),
    asset_id UUID REFERENCES assets(id) ON DELETE SET NULL,
    storage_key TEXT NOT NULL,
    path TEXT NOT NULL CHECK (path ~ '^\.kuayle/attachments/[A-Za-z0-9._-]{1,120}$'),
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL CHECK (size > 0 AND size <= 10485760),
    PRIMARY KEY (run_id, position)
);
CREATE INDEX idx_agent_task_attachments_task ON agent_task_attachments(task_id);

-- Off by default: images are only sent to the model once the owner says the
-- configured model can see them.
ALTER TABLE ai_settings ADD COLUMN assistant_vision BOOLEAN NOT NULL DEFAULT FALSE;
