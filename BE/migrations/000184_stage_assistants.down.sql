ALTER TABLE agent_task_workflow_steps
    DROP COLUMN stage_input_run_id,
    DROP COLUMN stage_first_run_id,
    DROP COLUMN stage_merge_observed;
