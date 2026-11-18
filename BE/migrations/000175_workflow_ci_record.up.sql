-- What a workflow's CI check step judged about the published commit
-- (KUA-215): the commit, the failed checks it saw, the details Kuayle read
-- about failed check runs and the re-run of failed jobs Kuayle asked for when
-- nothing had changed since CI failed on that commit. A later CI step on the
-- same commit reads it, so a failure already judged is not taken as a new
-- result. Steps recorded before this migration have none.
ALTER TABLE agent_task_workflow_steps ADD COLUMN ci JSONB CHECK (ci IS NULL OR octet_length(ci::text) <= 16384);
-- A details read finds the open CI steps waiting for it by commit.
CREATE INDEX agent_task_workflow_steps_ci_head ON agent_task_workflow_steps ((ci->>'head')) WHERE ci IS NOT NULL AND finished_at IS NULL;
