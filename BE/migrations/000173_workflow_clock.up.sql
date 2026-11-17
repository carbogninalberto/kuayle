-- The workflow's time budget counts working time only (KUA-171). It starts
-- when a task first leaves queued for real work (preparing), so a blocked wait
-- never counts, and it stops while a task waits on a person (an approval or a
-- merge, or waiting_input). workflow_held_since records when such a wait began;
-- releasing it moves the budget start forward by the time it was held.
ALTER TABLE agent_tasks ADD COLUMN workflow_held_since TIMESTAMPTZ;

-- A task still queued has never done real work: give it a fresh clock when it
-- starts. Only a task whose history proves it never reached preparing is reset;
-- one that did is already counted from its first preparing transition.
UPDATE agent_tasks t SET workflow_started_at=NULL
 WHERE t.state='queued'
   AND t.workflow_started_at IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM agent_task_transitions x WHERE x.task_id=t.id AND x.to_state='preparing');

-- A task waiting right now at an approval or merge step is held from the time
-- it entered that step, so its overnight wait does not use up its budget.
UPDATE agent_tasks t SET workflow_held_since=s.entered_at
  FROM agent_task_workflow_steps s
 WHERE s.task_id=t.id
   AND s.sequence=t.workflow_sequence
   AND s.finished_at IS NULL
   AND s.node_type IN ('approval','merge')
   AND t.state='awaiting_review'
   AND t.workflow_status='waiting';
