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
    ADD COLUMN failure_class TEXT,
    ADD COLUMN failure_rule TEXT,
    ADD COLUMN failure_cause TEXT,
    ADD COLUMN environment JSONB,
    ADD COLUMN output_cuts JSONB NOT NULL DEFAULT '[]'::jsonb;
-- Add the checks as NOT VALID and validate them separately: adding a checked
-- column to a large table otherwise scans and locks it for the whole ALTER.
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_failure_class_check
    CHECK (failure_class IN ('agent','provider','platform','ci_infrastructure','policy','person','unknown')) NOT VALID;
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_failure_rule_check
    CHECK (char_length(failure_rule) <= 64) NOT VALID;
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_failure_cause_check
    CHECK (octet_length(failure_cause) <= 4096) NOT VALID;
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_environment_check
    CHECK (octet_length(environment::text) <= 16384) NOT VALID;
ALTER TABLE dev_machine_agent_runs ADD CONSTRAINT dev_machine_agent_runs_output_cuts_check
    CHECK (octet_length(output_cuts::text) <= 16384) NOT VALID;
ALTER TABLE dev_machine_agent_runs VALIDATE CONSTRAINT dev_machine_agent_runs_failure_class_check;
ALTER TABLE dev_machine_agent_runs VALIDATE CONSTRAINT dev_machine_agent_runs_failure_rule_check;
ALTER TABLE dev_machine_agent_runs VALIDATE CONSTRAINT dev_machine_agent_runs_failure_cause_check;
ALTER TABLE dev_machine_agent_runs VALIDATE CONSTRAINT dev_machine_agent_runs_environment_check;
ALTER TABLE dev_machine_agent_runs VALIDATE CONSTRAINT dev_machine_agent_runs_output_cuts_check;

-- The class of the failure a task is failing or failed with; its reason is
-- the cause. Cleared when the task is retried.
ALTER TABLE agent_tasks ADD COLUMN failure_class TEXT;
ALTER TABLE agent_tasks ADD CONSTRAINT agent_tasks_failure_class_check
    CHECK (failure_class IN ('agent','provider','platform','ci_infrastructure','policy','person','unknown')) NOT VALID;
ALTER TABLE agent_tasks VALIDATE CONSTRAINT agent_tasks_failure_class_check;

-- A failed CI signal's class and the evidence that decided it. A failure of
-- the runner or network is recorded with the action 'infrastructure' and
-- starts no follow-up.
ALTER TABLE agent_task_signals
    ADD COLUMN failure_class TEXT,
    ADD COLUMN failure_evidence TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_failure_class_check
    CHECK (failure_class IN ('test_failure','build_failure','runner_or_network','timeout','cancelled')) NOT VALID;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_failure_evidence_check
    CHECK (octet_length(failure_evidence) <= 2048) NOT VALID;
ALTER TABLE agent_task_signals VALIDATE CONSTRAINT agent_task_signals_failure_class_check;
ALTER TABLE agent_task_signals VALIDATE CONSTRAINT agent_task_signals_failure_evidence_check;
ALTER TABLE agent_task_signals DROP CONSTRAINT agent_task_signals_action_check;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_action_check
    CHECK (action IN ('recorded','follow_up','budget_exhausted','not_reviewable','untrusted','follow_up_failed','infrastructure'));
