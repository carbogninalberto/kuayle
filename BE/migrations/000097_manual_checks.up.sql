-- Run a task's check on demand, without an agent attempt.

-- run_check runs the check command in the credential-free check container on
-- the task's checkout. Its result is a manual_check step on the task's latest
-- attempt (dev_machine_agent_run_steps.step_type is free text, so the step
-- type needs no schema change); the operation's idempotency key names the step.
ALTER TYPE dev_machine_operation_action ADD VALUE IF NOT EXISTS 'run_check';

-- The assistant's run_check tool asks for approval first.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check'));
