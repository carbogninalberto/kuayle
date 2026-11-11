-- Paused workflows continue as if they had never been paused; the time they
-- spent paused counts against their budgets again.
ALTER TABLE agent_task_workflow_steps DROP COLUMN IF EXISTS paused_seconds;
ALTER TABLE agent_tasks DROP CONSTRAINT IF EXISTS agent_tasks_workflow_pause_check,
    DROP COLUMN IF EXISTS workflow_paused_at,
    DROP COLUMN IF EXISTS workflow_pause_request;
