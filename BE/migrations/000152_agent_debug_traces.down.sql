-- Signals recorded as a runner failure become plain recorded signals; the
-- causes, classes, environments and cut records are dropped.
UPDATE agent_task_signals SET action = 'recorded' WHERE action = 'infrastructure';
ALTER TABLE agent_task_signals DROP CONSTRAINT agent_task_signals_action_check;
ALTER TABLE agent_task_signals ADD CONSTRAINT agent_task_signals_action_check
    CHECK (action IN ('recorded','follow_up','budget_exhausted','not_reviewable','untrusted','follow_up_failed'));
ALTER TABLE agent_task_signals
    DROP COLUMN failure_class,
    DROP COLUMN failure_evidence;
ALTER TABLE agent_tasks DROP COLUMN failure_class;
ALTER TABLE dev_machine_agent_runs
    DROP COLUMN failure_class,
    DROP COLUMN failure_rule,
    DROP COLUMN failure_cause,
    DROP COLUMN environment,
    DROP COLUMN output_cuts;
