-- A waiting merge cannot exist under the older constraints: it ends as failed
-- with a reason the card shows. Nothing was merged by it, so nothing on GitHub
-- changes; the pull request keeps the branch update Kuayle made.
UPDATE conversation_approvals SET status = 'failed', resolved_at = now(), updated_at = now(), next_check_at = NULL,
    error = 'Kuayle stopped waiting for the checks of the updated branch, so the pull request was not merged. Ask to merge again once its checks pass'
    WHERE status = 'waiting_checks';
DROP INDEX IF EXISTS conversation_approvals_waiting;
ALTER TABLE conversation_approvals DROP CONSTRAINT IF EXISTS conversation_approvals_waiting_check;
ALTER TABLE conversation_approvals DROP COLUMN IF EXISTS next_check_at;
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_check
    CHECK ((status IN ('pending','approving')) = (resolved_at IS NULL));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_status_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_status_check
    CHECK (status IN ('pending','approving','approved','denied','expired','failed'));

-- A workflow step that waited for its merge keeps waiting, without the merge:
-- a person merges from chat or on GitHub, or approves again.
ALTER TABLE agent_task_workflow_steps DROP COLUMN IF EXISTS merge;
