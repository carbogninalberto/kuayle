-- Signals explain why automated follow-up attempts ran.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_task_signals) THEN
  RAISE EXCEPTION 'Cannot downgrade retained agent task signals';
 END IF;
END $$;
DROP TABLE agent_task_signals;
