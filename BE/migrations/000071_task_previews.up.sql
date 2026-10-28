-- Automatic QA previews: an inherited command and port, snapshotted onto each
-- task. When a task reaches review, the manager starts the command in the
-- task's worktree and points the machine's authenticated preview at the port.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'start_preview';
ALTER TABLE dev_machine_scope_settings
 ADD COLUMN preview_command jsonb CHECK (preview_command IS NULL OR (jsonb_typeof(preview_command)='array' AND jsonb_array_length(preview_command) BETWEEN 1 AND 64)),
 ADD COLUMN preview_port int CHECK (preview_port IS NULL OR preview_port BETWEEN 1024 AND 65535),
 ADD CONSTRAINT preview_complete CHECK ((preview_command IS NULL) = (preview_port IS NULL));
ALTER TABLE agent_tasks
 ADD COLUMN preview_command jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(preview_command)='array'),
 ADD COLUMN preview_port int CHECK (preview_port IS NULL OR preview_port BETWEEN 1024 AND 65535);
