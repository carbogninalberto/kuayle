-- Restores the fixed roles. Labels that are not one of the old roles are
-- lost: such a profile becomes a reviewer when it reviews and an implementer
-- otherwise, and a planning profile becomes an implementer. Stored plans and
-- the worktree comparison of past attempts are dropped.
ALTER TABLE dev_machine_agent_runs DROP COLUMN IF EXISTS worktree_changed_files,
    DROP COLUMN IF EXISTS worktree_changed,
    DROP COLUMN IF EXISTS worktree_before;
ALTER TABLE agent_task_workflow_steps DROP COLUMN IF EXISTS plan;

ALTER TABLE agent_profiles DROP CONSTRAINT IF EXISTS agent_profiles_role_check;
ALTER TABLE agent_profiles ALTER COLUMN role DROP DEFAULT;
UPDATE agent_profiles SET role = CASE
    WHEN behaviour = 'reviews' AND lower(replace(role, ' ', '_')) = 'security_reviewer' THEN 'security_reviewer'
    WHEN behaviour = 'reviews' THEN 'reviewer'
    WHEN behaviour = 'changes_code' AND lower(replace(role, ' ', '_')) IN ('tester', 'docs_writer') THEN lower(replace(role, ' ', '_'))
    ELSE 'implementer' END;
ALTER TABLE agent_profiles ADD CONSTRAINT agent_profiles_role_check
    CHECK (role IN ('implementer', 'reviewer', 'tester', 'docs_writer', 'security_reviewer'));
ALTER TABLE agent_profiles DROP COLUMN IF EXISTS behaviour;
