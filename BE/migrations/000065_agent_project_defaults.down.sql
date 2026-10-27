DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE agent_provider IS NOT NULL OR agent_instructions IS NOT NULL)
 OR EXISTS(SELECT 1 FROM agent_tasks WHERE agent_instructions<>'') THEN
  RAISE EXCEPTION 'Cannot downgrade retained agent defaults or task instruction snapshots';
 END IF;
END $$;
ALTER TABLE agent_tasks DROP COLUMN agent_instructions;
ALTER TABLE dev_machine_scope_settings DROP COLUMN agent_instructions, DROP COLUMN agent_provider;
