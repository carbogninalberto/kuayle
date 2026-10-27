ALTER TABLE agent_tasks DROP COLUMN preview_port, DROP COLUMN preview_command;
ALTER TABLE dev_machine_scope_settings DROP CONSTRAINT preview_complete, DROP COLUMN preview_port, DROP COLUMN preview_command;
-- The start_preview enum value cannot be removed; it is unused after rollback.
