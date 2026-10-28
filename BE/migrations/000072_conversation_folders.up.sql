-- Private folders that group a user's own conversations in the assistant.
CREATE TABLE conversation_folders (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (id, workspace_id)
);
CREATE INDEX conversation_folders_owner ON conversation_folders(workspace_id, owner_id, name);
ALTER TABLE conversations ADD COLUMN folder_id uuid REFERENCES conversation_folders(id) ON DELETE SET NULL;
