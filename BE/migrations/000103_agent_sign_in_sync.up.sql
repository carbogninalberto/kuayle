-- Saving or removing a user's agent sign-in queues a sync_agent_sign_ins
-- operation for each running machine that user owns. The manager then rewrites
-- the owner's sign-ins in the machine's terminal home, so the next terminal
-- shell uses the current sign-in.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'sync_agent_sign_ins';
-- When the user opens a task's Claude Code session in the task terminal, the
-- attempt that recorded it is marked, so the next attempt resumes the session
-- with the user's turns even when that attempt failed.
ALTER TABLE dev_machine_agent_runs ADD COLUMN IF NOT EXISTS session_continued_at TIMESTAMPTZ;
