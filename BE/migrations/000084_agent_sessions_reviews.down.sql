-- Review tasks would otherwise become ordinary implementation tasks that could
-- validate and publish; refuse to drop them. Sessions are derived records.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM agent_tasks WHERE kind='review') THEN
  RAISE EXCEPTION 'Cannot downgrade retained agent review tasks';
 END IF;
END $$;
DELETE FROM conversation_approvals WHERE action IN ('request_agent_review','follow_up_task');
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request'));
DROP TABLE agent_reviews;
ALTER TABLE dev_machine_agent_runs DROP COLUMN session_id;
DROP TABLE agent_sessions;
DROP INDEX agent_tasks_review_of;
ALTER TABLE agent_tasks DROP CONSTRAINT agent_tasks_review_target;
ALTER TABLE agent_tasks DROP COLUMN review_of_task_id;
ALTER TABLE agent_tasks DROP COLUMN kind;
