-- PostgreSQL enum values cannot be removed in place. Retain the inert value,
-- but refuse rollback while manual check operations or results still exist.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_operations WHERE action::text='run_check')
 OR EXISTS(SELECT 1 FROM dev_machine_agent_run_steps WHERE step_type='manual_check') THEN
  RAISE EXCEPTION 'Cannot downgrade retained manual checks';
 END IF;
END $$;
-- Check approvals are discarded; the checks they ran are refused above.
DELETE FROM conversation_approvals WHERE action='run_check';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port'));
