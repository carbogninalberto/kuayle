-- Inherited Compose file for independent checks. The path is snapshotted onto
-- each task and its check step, like the test command.
ALTER TABLE dev_machine_scope_settings ADD COLUMN compose_file text CHECK (
 compose_file IS NULL OR (char_length(compose_file) <= 255
  AND compose_file ~ '^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*\.ya?ml$'
  AND compose_file !~ '(^|/)\.\.?(/|$)'));
ALTER TABLE agent_tasks ADD COLUMN compose_file text NOT NULL DEFAULT '' CHECK (char_length(compose_file) <= 255);
ALTER TABLE dev_machine_agent_run_steps ADD COLUMN compose_file text NOT NULL DEFAULT '' CHECK (char_length(compose_file) <= 255);
