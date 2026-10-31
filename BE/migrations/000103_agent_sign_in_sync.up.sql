-- Saving or removing a user's agent sign-in queues a sync_agent_sign_ins
-- operation for each running machine that user owns. The manager then rewrites
-- the owner's sign-ins in the machine's terminal home, so the next terminal
-- shell uses the current sign-in.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'sync_agent_sign_ins';
