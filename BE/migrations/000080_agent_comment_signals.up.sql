-- Kuayle issue comments by workspace members become a third signal source:
-- a comment on an issue whose latest task awaits review queues a bounded
-- follow-up attempt, like a trusted GitHub change request.
ALTER TABLE agent_task_signals DROP CONSTRAINT agent_task_signals_kind_check;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_kind_check CHECK (kind IN ('ci','review','comment'));
