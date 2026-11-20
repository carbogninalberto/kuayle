ALTER TABLE agent_tasks
    ADD COLUMN workflow_switch_sequence INTEGER NOT NULL DEFAULT 0,
    DROP CONSTRAINT agent_tasks_workflow_status_check,
    ADD CONSTRAINT agent_tasks_workflow_status_check
        CHECK (workflow_status IN ('entering', 'running', 'waiting', 'paused', 'done', 'failed'));
