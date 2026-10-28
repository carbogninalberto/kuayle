-- Whether a git ref was pushed by Kuayle's publication or only observed in the
-- agent-writable checkout. Publication builds only on published commits.
ALTER TABLE dev_machine_git_refs
 ADD COLUMN source varchar(16) NOT NULL DEFAULT 'observed' CHECK (source IN ('observed', 'published'));
-- Publications record their refs in one transaction, beside a pull request ref.
UPDATE dev_machine_git_refs g SET source='published'
 WHERE EXISTS (SELECT 1 FROM dev_machine_git_refs p WHERE p.agent_run_id=g.agent_run_id
   AND p.ref_type='pull_request' AND p.created_at=g.created_at);
