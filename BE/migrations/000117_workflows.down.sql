-- Tasks that ran a configurable workflow cannot be represented without it:
-- refuse to roll back while any exist rather than silently switching them to
-- the Standard flow.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agent_tasks WHERE workflow_version_id IS NOT NULL) THEN
        RAISE EXCEPTION 'agent tasks still run configurable workflows';
    END IF;
END $$;
DROP TABLE agent_task_workflow_steps;
DROP INDEX agent_tasks_workflow_wake;
ALTER TABLE agent_tasks
    DROP CONSTRAINT agent_tasks_workflow_cursor,
    DROP COLUMN workflow_sequence,
    DROP COLUMN workflow_wake_at,
    DROP COLUMN workflow_status,
    DROP COLUMN workflow_started_at,
    DROP COLUMN workflow_iterations,
    DROP COLUMN workflow_visits,
    DROP COLUMN workflow_node,
    DROP COLUMN workflow_version_id;
DROP TABLE workflow_audit_events;
DROP TABLE workflow_defaults;
DROP TABLE workflow_versions;
DROP FUNCTION workflow_versions_immutable();
DROP TABLE workflows;
DROP TABLE agent_profiles;
