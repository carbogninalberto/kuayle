-- Agent profiles: a behaviour (plans, changes code, reviews) replaces the
-- fixed list of roles, and the role becomes a label the workspace chooses
-- (KUA-124). Reviewer and security reviewer profiles review; every other
-- profile changes code, as it did. The old role stays as the label.
ALTER TABLE agent_profiles ADD COLUMN behaviour TEXT NOT NULL DEFAULT 'changes_code'
    CHECK (behaviour IN ('plans', 'changes_code', 'reviews'));
UPDATE agent_profiles SET behaviour = 'reviews' WHERE role IN ('reviewer', 'security_reviewer');

ALTER TABLE agent_profiles DROP CONSTRAINT IF EXISTS agent_profiles_role_check;
UPDATE agent_profiles SET role = CASE role
    WHEN 'implementer' THEN 'Implementer'
    WHEN 'reviewer' THEN 'Reviewer'
    WHEN 'tester' THEN 'Tester'
    WHEN 'docs_writer' THEN 'Docs writer'
    WHEN 'security_reviewer' THEN 'Security reviewer'
    ELSE role END;
ALTER TABLE agent_profiles ALTER COLUMN role SET DEFAULT '';
ALTER TABLE agent_profiles ADD CONSTRAINT agent_profiles_role_check
    CHECK (char_length(role) <= 40 AND role = btrim(role));

-- A planning step's plan, kept whole for the agent steps after it.
ALTER TABLE agent_task_workflow_steps ADD COLUMN plan TEXT;

-- Whether the attempt changed its worktree (commits included) and which
-- files, from Kuayle's fingerprint of the worktree before the agent started
-- and after it ended; NULL when the worktree could not be compared.
ALTER TABLE dev_machine_agent_runs ADD COLUMN worktree_before TEXT,
    ADD COLUMN worktree_changed BOOLEAN,
    ADD COLUMN worktree_changed_files JSONB NOT NULL DEFAULT '[]';
