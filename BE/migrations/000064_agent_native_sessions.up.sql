-- Native harness session of a task attempt, for resuming the next attempt.
ALTER TABLE dev_machine_agent_runs ADD COLUMN native_session_id text CHECK (native_session_id ~ '^[A-Za-z0-9][A-Za-z0-9-]{0,127}$');
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'purge_task_session';
