-- Debug traces (KUA-137): every failure has a cause and a class, every run
-- records what it ran on and what Kuayle cut from its output, and a CI
-- failure says whether it was the change or the runner.

-- A run that did not succeed: failure_class is who or what stopped it,
-- failure_rule the classification rule that decided, failure_cause the real
-- message (exit code and signal, the container's end state, the provider's
-- error, the end of standard error), redacted and bounded. environment is
-- what the run ran on (domain.AgentRunEnvironment): harness and version,
-- image and digest, limits, the session it resumed. output_cuts lists what
-- Kuayle left out of the run's stored output (domain.OutputCut). Runs that
-- finished before this migration keep NULL: their cause was not recorded.
ALTER TABLE dev_machine_agent_runs
    ADD COLUMN failure_class TEXT CHECK (failure_class IN ('agent','provider','platform','ci_infrastructure','policy','person','unknown')),
    ADD COLUMN failure_rule TEXT CHECK (char_length(failure_rule) <= 64),
    ADD COLUMN failure_cause TEXT CHECK (octet_length(failure_cause) <= 4096),
    ADD COLUMN environment JSONB CHECK (octet_length(environment::text) <= 16384),
    ADD COLUMN output_cuts JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (octet_length(output_cuts::text) <= 16384);
CREATE INDEX dev_machine_agent_runs_failures ON dev_machine_agent_runs (workspace_id, completed_at)
    WHERE status IN ('failed', 'timeout') AND task_id IS NOT NULL;

-- The class of the failure a task is failing or failed with; its reason is
-- the cause. Cleared when the task is retried.
ALTER TABLE agent_tasks
    ADD COLUMN failure_class TEXT CHECK (failure_class IN ('agent','provider','platform','ci_infrastructure','policy','person','unknown'));

-- A failed CI signal's class and the evidence that decided it. A failure of
-- the runner or network is recorded with the action 'infrastructure' and
-- starts no follow-up.
ALTER TABLE agent_task_signals
    ADD COLUMN failure_class TEXT CHECK (failure_class IN ('test_failure','build_failure','runner_or_network','timeout','cancelled')),
    ADD COLUMN failure_evidence TEXT NOT NULL DEFAULT '' CHECK (octet_length(failure_evidence) <= 2048);
ALTER TABLE agent_task_signals DROP CONSTRAINT agent_task_signals_action_check;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_action_check
    CHECK (action IN ('recorded','follow_up','budget_exhausted','not_reviewable','untrusted','follow_up_failed','infrastructure'));
