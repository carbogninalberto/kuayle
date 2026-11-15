-- Automatic retries of a task step after a platform failure (a machine
-- manager restart), which never count against the workflow's budget.
ALTER TABLE agent_tasks
    ADD COLUMN platform_retries INTEGER NOT NULL DEFAULT 0 CHECK (platform_retries >= 0),
    ADD COLUMN platform_retry_pending BOOLEAN NOT NULL DEFAULT false;
