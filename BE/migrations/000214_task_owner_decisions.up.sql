-- Old structured requests had no prose when the author supplied only --request.
UPDATE task_messages SET body = CASE WHEN data->>'action' = 'split'
    THEN 'Create the proposed sub-issues' ELSE 'Request to ' || (data->>'action') END
WHERE kind = 'request' AND body = '' AND reject_reason IS NULL AND data ? 'action';

CREATE INDEX task_messages_decision_replies ON task_messages(task_id,reply_to)
WHERE reply_to IS NOT NULL AND reject_reason IS NULL;

-- Announce committed Inbox entries even when the pipeline tick is paused.
CREATE TABLE task_decision_notices (
 notification_id uuid PRIMARY KEY REFERENCES notifications(id) ON DELETE CASCADE,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE
);

-- Earlier ticks only recorded the cause in the reason. Mark existing stalls once.
UPDATE agent_tasks SET pipeline_state=jsonb_set(pipeline_state,'{stall,paused}','true')
WHERE pipeline AND state='blocked' AND workflow_status='paused'
  AND reason LIKE 'Workflow · Nobody made progress on %';

CREATE VIEW open_room_decisions AS
SELECT t.id AS task_id, m.seq, m.kind, m.author_role AS asker, m.body, m.data,
  (t.pipeline AND t.state='blocked' AND t.workflow_status='paused' AND t.workflow_paused_at IS NULL
    AND t.pipeline_state @> '{"stall":{"paused":true}}') IS TRUE AS blocks_pause
FROM task_messages m JOIN agent_tasks t ON t.id=m.task_id
WHERE t.state NOT IN ('completed','failed','cancelled','cancelling')
  AND m.author_kind='member' AND m.reject_reason IS NULL
  AND (('human'=ANY(m.to_roles) AND (m.kind='ask' OR (m.kind='request' AND m.data->>'action'<>'approve')))
       OR (m.kind='request' AND m.data->>'action'='split'))
  AND NOT EXISTS (
    SELECT 1 FROM task_messages a WHERE a.task_id=t.id AND a.reply_to=m.seq AND a.reject_reason IS NULL
      AND ((a.author_kind='human' AND a.author_user_id=t.creator_id AND a.kind='say'
            AND (m.kind='ask' OR m.data->>'action' IS DISTINCT FROM 'split' OR a.data ? 'answer'))
           OR (a.author_kind='kuayle' AND a.kind='fact' AND a.data->>'fact'='split_approved')))
  AND (m.kind='ask' OR m.data->>'action' IS DISTINCT FROM 'split' OR NOT EXISTS (
    SELECT 1 FROM task_messages g WHERE g.task_id=t.id AND g.seq<m.seq AND g.author_kind='human'
      AND g.author_user_id=t.creator_id AND g.kind='grant' AND g.reject_reason IS NULL
      AND g.data @> '{"role":"lead","action":"split"}')
    OR EXISTS (SELECT 1 FROM task_messages f WHERE f.task_id=t.id AND f.reply_to=m.seq AND f.author_kind='kuayle'
      AND f.kind='fact' AND f.data->>'fact'='split_refused'));

CREATE VIEW task_owner_decisions AS
SELECT * FROM open_room_decisions
UNION ALL
SELECT t.id, t.workflow_sequence::bigint, 'pause', 'Kuayle', t.reason, '{}'::jsonb, false
FROM agent_tasks t WHERE t.pipeline AND t.workflow_status='paused' AND t.workflow_paused_at IS NULL
  AND t.state IN ('blocked','awaiting_review')
  AND NOT EXISTS (SELECT 1 FROM open_room_decisions r WHERE r.task_id=t.id AND r.blocks_pause)
UNION ALL
SELECT t.id, t.workflow_sequence::bigint, 'approval', 'Kuayle', phase->>'label',
  jsonb_build_object('commit',t.pipeline_state->'facts'->>'head_sha'), false
FROM agent_tasks t JOIN workflow_versions v ON v.id=t.workflow_version_id
CROSS JOIN LATERAL jsonb_array_elements(v.graph->'phases') WITH ORDINALITY current_phase(phase,pos)
WHERE t.pipeline AND t.workflow_status='waiting' AND t.state='awaiting_review' AND t.workflow_paused_at IS NULL
  AND phase->>'id'=t.workflow_node AND COALESCE(t.pipeline_state->'facts'->>'head_sha','')<>''
  AND t.pipeline_state->'approved'->>'head' IS DISTINCT FROM t.pipeline_state->'facts'->>'head_sha'
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(v.graph->'phases') WITH ORDINALITY p(value,idx)
    WHERE (idx=pos OR (v.graph->'lead'->>'profile_id' IS NOT NULL AND idx<pos))
      AND value->'checks' @> '[{"fact":"approved"}]');
