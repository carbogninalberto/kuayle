-- Refuse to silently lose configured commands or accepted task snapshots.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE test_command IS NOT NULL)
 OR EXISTS(SELECT 1 FROM agent_tasks WHERE test_command IS NOT NULL OR validation_command<>'[]'::jsonb) THEN
  RAISE EXCEPTION 'Cannot remove configured or accepted test commands';
 END IF;
END $$;
ALTER TABLE agent_tasks DROP COLUMN validation_command, DROP COLUMN test_command;
ALTER TABLE dev_machine_scope_settings DROP COLUMN test_command;
