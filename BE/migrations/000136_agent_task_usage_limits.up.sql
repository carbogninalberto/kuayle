-- A task whose agent hit its provider's usage limit (e.g. Claude Code's
-- 5-hour limit) is paused rather than failed: it is blocked with the
-- provider's message and the reset time, keeps its worktree and session, and
-- resumes on its own at usage_limit_resume_at.
ALTER TABLE agent_tasks
  ADD COLUMN usage_limit_message text CHECK (char_length(usage_limit_message) <= 500),
  ADD COLUMN usage_limit_resets_at timestamptz,
  -- NULL: no automatic resume (the user turned it off, or the cap was hit).
  ADD COLUMN usage_limit_resume_at timestamptz,
  -- Consecutive automatic resumes; reset by a manual retry.
  ADD COLUMN usage_limit_resumes integer NOT NULL DEFAULT 0 CHECK (usage_limit_resumes >= 0),
  -- The next attempt continues the previous attempt's native session.
  ADD COLUMN resume_native_session boolean NOT NULL DEFAULT false;

CREATE INDEX agent_tasks_usage_limit_resume_idx ON agent_tasks (usage_limit_resume_at)
  WHERE state = 'blocked' AND usage_limit_resume_at IS NOT NULL;

-- One user's agents share one subscription: while it is limited, that user's
-- queued tasks for the same agent wait instead of starting and failing.
CREATE TABLE agent_usage_limit_holds (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider text NOT NULL,
  resets_at timestamptz NOT NULL,
  message text NOT NULL CHECK (char_length(message) <= 500),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, provider)
);
