-- Downgrade must not discard the ownership of a live execution. Cancel tasks
-- and allow their workers to confirm runtime cleanup before rolling back.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM dev_machine_agent_runs WHERE encrypted_output_secrets IS NOT NULL AND status IN ('queued','starting','running','waiting_input')) THEN
  RAISE EXCEPTION 'Stop native attempts before removing their recovery credentials';
 END IF;
 IF EXISTS (SELECT 1 FROM agent_tasks WHERE state NOT IN ('completed','cancelled','failed','blocked','awaiting_review')) THEN
  RAISE EXCEPTION 'Cancel active agent tasks and wait for runtime cleanup before rollback';
 END IF;
 IF EXISTS (SELECT 1 FROM dev_machine_checkouts WHERE scope_key<>'') THEN
  RAISE EXCEPTION 'Remove task worktrees through machine cleanup before rollback';
 END IF;
END $$;
ALTER TABLE dev_machine_checkouts DROP CONSTRAINT dev_machine_checkout_scope;
ALTER TABLE dev_machine_checkouts DROP COLUMN scope_key;
ALTER TABLE dev_machine_checkouts ADD UNIQUE(machine_id,issue_id);
ALTER TABLE dev_machine_agent_runs DROP COLUMN task_id;
ALTER TABLE dev_machine_agent_runs DROP COLUMN encrypted_output_secrets;
ALTER TABLE dev_machine_log_chunks DROP COLUMN source_key;
DROP TABLE agent_task_retries;
DROP TABLE agent_task_transitions;
DROP TABLE agent_tasks;
