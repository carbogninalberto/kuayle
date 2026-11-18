-- KUA-165: one-shot task watches. A watch is registered by the assistant tool
-- watch_agent_work in one user's conversation. The transaction that records
-- the event a watch waits for marks it due (due_state), with the same rule as
-- the creator's notice: an end state, or a wait for a person in
-- awaiting_review. notified records that the creator's notice for that event
-- already reaches the watcher. A claimed watch row is deleted, which makes the
-- ping one-shot. until_state NULL means any end state (completed, failed or
-- cancelled).
CREATE TABLE agent_task_watchers (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 parent_message_id uuid NOT NULL REFERENCES conversation_messages(id) ON DELETE CASCADE,
 until_state text CHECK (until_state IN ('awaiting_review','completed','failed')),
 due_state text CHECK (due_state IN ('awaiting_review','completed','failed','cancelled')),
 notified boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 -- One live watch per user, conversation and task: registering again replaces
 -- it, so a repeated request still pings exactly once.
 UNIQUE (task_id, user_id, conversation_id)
);
CREATE INDEX agent_task_watchers_due ON agent_task_watchers(created_at, id) WHERE due_state IS NOT NULL;
