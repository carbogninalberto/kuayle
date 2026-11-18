-- A task whose provider account has no credit left (KUA-218) is paused like
-- one at a usage limit: blocked, with the provider's message in
-- usage_limit_message and its worktree and session kept. This marks that
-- pause: it has no reset time and no automatic resume, holds no other task,
-- and resuming continues the same workflow step, visit and iteration.
ALTER TABLE agent_tasks ADD COLUMN provider_credit_exhausted boolean NOT NULL DEFAULT false;
