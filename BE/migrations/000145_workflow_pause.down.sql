-- Workflows paused in place (a waiting step, or after a step) continue as if
-- they had never been paused. A task whose agent "pause now" stopped stays
-- blocked ("Paused") and needs a Retry. Time spent in a pause that is still
-- open counts against the budgets again.
ALTER TABLE agent_task_workflow_steps DROP COLUMN IF EXISTS paused_seconds;
ALTER TABLE agent_tasks DROP CONSTRAINT IF EXISTS agent_tasks_workflow_pause_check,
    DROP COLUMN IF EXISTS workflow_paused_at,
    DROP COLUMN IF EXISTS workflow_pause_request;
