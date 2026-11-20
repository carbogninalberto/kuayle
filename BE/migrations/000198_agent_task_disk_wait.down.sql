UPDATE agent_tasks SET state='preparing',reason='Starting the dev machine',next_poll_at=now(),
    workflow_budget_from=CASE WHEN workflow_held_since IS NULL THEN workflow_budget_from
        ELSE COALESCE(workflow_budget_from,workflow_started_at)+(now()-workflow_held_since) END,
    workflow_held_since=NULL
WHERE state='waiting_for_disk';

ALTER TABLE agent_tasks DROP CONSTRAINT agent_tasks_state_check;
ALTER TABLE agent_tasks ADD CONSTRAINT agent_tasks_state_check CHECK (state IN (
    'queued','preparing','running','waiting_input','validating','publishing',
    'awaiting_review','blocked','completed','failed','cancelling','cancelled'
));
