-- PostgreSQL enum values cannot be removed in place. Retain the inert value,
-- but refuse rollback while tool run operations are still queued or running.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dev_machine_operations WHERE action::text='run_tool' AND status IN ('pending','leased')) THEN
  RAISE EXCEPTION 'Cannot downgrade while custom tools are running';
 END IF;
END $$;
-- Tool approvals are discarded; their tool calls stay in the conversation.
DELETE FROM conversation_approvals WHERE action='run_custom_tool';
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check'));
-- Calls of custom tools whose names have digits keep their record under a
-- generic name that the earlier constraint allows.
UPDATE conversation_tool_calls SET name='custom_tool' WHERE name !~ '^[a-z_]{1,64}$';
ALTER TABLE conversation_tool_calls DROP CONSTRAINT conversation_tool_calls_name_check;
ALTER TABLE conversation_tool_calls ADD CONSTRAINT conversation_tool_calls_name_check CHECK (name ~ '^[a-z_]{1,64}$');
DROP TABLE IF EXISTS assistant_tool_runs;
