-- An earlier version would run only the first of an agent-chosen check's
-- commands, so rollback is refused while such a check is queued or running.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_agent_run_steps WHERE command_source='agent' AND status IN ('queued','running')) THEN
  RAISE EXCEPTION 'Cannot downgrade while agent-chosen checks are running';
 END IF;
END $$;
-- Agent-chosen commands, their per-command results and the agent's summary
-- are discarded; each step keeps its first command and overall exit code.
ALTER TABLE dev_machine_agent_run_steps
    DROP COLUMN IF EXISTS agent_summary,
    DROP COLUMN IF EXISTS command_results,
    DROP COLUMN IF EXISTS commands,
    DROP COLUMN IF EXISTS command_source;
ALTER TABLE dev_machine_agent_runs DROP COLUMN IF EXISTS declared_validation;
