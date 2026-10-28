-- Comment signals explain why follow-up attempts ran; refuse to drop them.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_task_signals WHERE kind='comment') THEN
  RAISE EXCEPTION 'Cannot downgrade retained comment signals';
 END IF;
END $$;
ALTER TABLE agent_task_signals DROP CONSTRAINT agent_task_signals_kind_check;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_kind_check CHECK (kind IN ('ci','review'));
