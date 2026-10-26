-- Keep the requested override separate from the accepted effective command.
ALTER TABLE dev_machine_scope_settings ADD COLUMN test_command jsonb
 CHECK(test_command IS NULL OR jsonb_typeof(test_command)='array');
ALTER TABLE agent_tasks ADD COLUMN test_command jsonb
 CHECK(test_command IS NULL OR jsonb_typeof(test_command)='array');
ALTER TABLE agent_tasks ADD COLUMN validation_command jsonb NOT NULL DEFAULT '[]'
 CHECK(jsonb_typeof(validation_command)='array');
