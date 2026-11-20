-- A switch awaiting worker entry must settle before rolling back.
ALTER TABLE agent_tasks
    DROP COLUMN workflow_switch_sequence,
    DROP CONSTRAINT agent_tasks_workflow_status_check,
    ADD CONSTRAINT agent_tasks_workflow_status_check
        CHECK (workflow_status IN ('running', 'waiting', 'paused', 'done', 'failed'));
