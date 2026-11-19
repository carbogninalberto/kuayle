ALTER TABLE agent_task_workflow_steps
    ADD COLUMN stage_input_run_id UUID,
    ADD COLUMN stage_first_run_id UUID,
    ADD COLUMN stage_merge_observed BOOLEAN NOT NULL DEFAULT false;
