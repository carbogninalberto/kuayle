-- Inherited coding-agent defaults. Instructions are snapshotted onto each task
-- when accepted, so later edits never change work already in progress.
ALTER TABLE dev_machine_scope_settings
 ADD COLUMN agent_provider text CHECK (agent_provider IS NULL OR agent_provider ~ '^[a-z-]{1,64}$'),
 ADD COLUMN agent_instructions text CHECK (agent_instructions IS NULL OR char_length(agent_instructions) BETWEEN 1 AND 8000);
ALTER TABLE agent_tasks ADD COLUMN agent_instructions text NOT NULL DEFAULT '' CHECK (char_length(agent_instructions) <= 8000);
