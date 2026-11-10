DROP TABLE IF EXISTS agent_usage_limit_holds;
DROP INDEX IF EXISTS agent_tasks_usage_limit_resume_idx;
ALTER TABLE agent_tasks
  DROP COLUMN IF EXISTS resume_native_session,
  DROP COLUMN IF EXISTS usage_limit_resumes,
  DROP COLUMN IF EXISTS usage_limit_resume_at,
  DROP COLUMN IF EXISTS usage_limit_resets_at,
  DROP COLUMN IF EXISTS usage_limit_message;
