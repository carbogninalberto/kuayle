-- Workflows are pipelines only. Graph workflows (the state-machine editor,
-- including a workspace's edited copy of the built-in Standard workflow) are
-- removed. A task without a workflow runs the built-in Standard flow, as
-- before; pipelines are untouched.

CREATE TEMPORARY TABLE removed_graph_versions ON COMMIT DROP AS
SELECT v.id, v.workflow_id, v.version
FROM workflow_versions v
WHERE COALESCE(v.graph->>'kind', 'graph') <> 'pipeline';

CREATE TEMPORARY TABLE removed_graph_workflows ON COMMIT DROP AS
SELECT w.id FROM workflows w
WHERE NOT EXISTS (
    SELECT 1 FROM workflow_versions v
    WHERE v.workflow_id = w.id AND COALESCE(v.graph->>'kind', 'graph') = 'pipeline');

-- Tasks that ran a graph keep their history (state, steps, transitions) but no
-- longer name its version. A task still in progress continues on the Standard
-- flow: its workflow cursor, budget, pause and wake are cleared, so the task
-- worker treats it like any task without a workflow (a running attempt
-- finishes and is published, awaiting_review waits for a person).
UPDATE agent_tasks t SET
    workflow_version_id = NULL,
    workflow_node = NULL,
    workflow_status = NULL,
    workflow_wake_at = NULL,
    workflow_pause_request = NULL,
    workflow_paused_at = NULL,
    workflow_held_since = NULL,
    workflow_budget_from = NULL,
    workflow_budget_extensions = 0,
    workflow_visits = '{}'::jsonb,
    workflow_iterations = 0,
    workflow_started_at = NULL,
    updated_at = now()
WHERE t.workflow_version_id IN (SELECT id FROM removed_graph_versions);

-- Automations that started tasks on a graph start them on the scope's
-- default (Standard or a pipeline default) instead.
UPDATE agent_automations a SET workflow_id = NULL, pinned_version = NULL
WHERE a.workflow_id IN (SELECT id FROM removed_graph_workflows);

-- Removing a workflow removes its versions and the scope defaults that chose
-- it (cascade); those scopes fall back to Standard.
DELETE FROM workflows WHERE id IN (SELECT id FROM removed_graph_workflows);
