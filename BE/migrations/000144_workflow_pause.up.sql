-- Pausing a running workflow (KUA-129). workflow_pause_request is a pause a
-- person asked for that has not taken effect yet: "now" while the task
-- finishes work Kuayle runs itself (a check, a publication), "after_step"
-- until the current step has an outcome. workflow_paused_at is set while the
-- workflow is paused: the worker leaves the task alone until it is resumed.
ALTER TABLE agent_tasks
    ADD COLUMN workflow_pause_request TEXT CHECK (workflow_pause_request IN ('now', 'after_step')),
    ADD COLUMN workflow_paused_at TIMESTAMPTZ,
    ADD CONSTRAINT agent_tasks_workflow_pause_check CHECK (workflow_pause_request IS NULL OR workflow_paused_at IS NULL);

-- Time a step spent paused, which its own timeout does not count.
ALTER TABLE agent_task_workflow_steps ADD COLUMN paused_seconds INTEGER NOT NULL DEFAULT 0 CHECK (paused_seconds >= 0);
