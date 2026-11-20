ALTER TABLE agent_tasks DROP CONSTRAINT agent_tasks_state_check;
ALTER TABLE agent_tasks ADD CONSTRAINT agent_tasks_state_check CHECK (state IN (
    'queued','preparing','waiting_for_disk','running','waiting_input','validating','publishing',
    'awaiting_review','blocked','completed','failed','cancelling','cancelled'
));
