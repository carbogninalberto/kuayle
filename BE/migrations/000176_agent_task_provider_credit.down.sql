-- A task still paused for credit then reads as paused by a usage limit with
-- automatic resume off: resuming it starts a new visit of its step.
ALTER TABLE agent_tasks DROP COLUMN IF EXISTS provider_credit_exhausted;
