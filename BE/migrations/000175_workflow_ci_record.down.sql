DROP INDEX IF EXISTS agent_task_workflow_steps_ci_head;
ALTER TABLE agent_task_workflow_steps DROP COLUMN IF EXISTS ci;
