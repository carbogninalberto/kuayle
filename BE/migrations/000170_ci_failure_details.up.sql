-- What a failed CI suite says about its failing checks (KUA-206). summary
-- (already bounded to 8 KiB) now also holds a CI signal's failure summary:
-- the failed checks, their failed steps, annotations and the end of the
-- failed step's log. head_sha is the commit the suite ran on, so a review
-- is told the CI of the commit it reviews. failure_details_pending is set
-- while Kuayle still reads the failed checks from GitHub outside the
-- webhook; the follow-up and the workflow's CI step wait for it, at most a
-- few minutes. Signals recorded before this migration have neither.
ALTER TABLE agent_task_signals
    ADD COLUMN head_sha TEXT NOT NULL DEFAULT '',
    ADD COLUMN failure_details_pending BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_head_sha_check
    CHECK (char_length(head_sha) <= 64) NOT VALID;
ALTER TABLE agent_task_signals VALIDATE CONSTRAINT agent_task_signals_head_sha_check;
-- The details read finds a commit's waiting signals.
CREATE INDEX agent_task_signals_details_pending ON agent_task_signals (head_sha) WHERE failure_details_pending;
