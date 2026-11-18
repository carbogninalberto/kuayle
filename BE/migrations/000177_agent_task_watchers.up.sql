-- KUA-165: one-shot task watches. A watch is registered by the assistant tool
-- watch_agent_work in one user's conversation; from_sequence is the task's last
-- recorded transition when it was registered, so a watch never fires for a
-- state the task had already reached (the tool reports the current state
-- instead). A claimed watch row is deleted, which makes the ping one-shot.
-- until_state NULL means any end state (completed, failed or cancelled).
-- Rolling back drops only pending watches; nothing else is lost.
CREATE TABLE agent_task_watchers (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 parent_message_id uuid NOT NULL REFERENCES conversation_messages(id) ON DELETE CASCADE,
 until_state text CHECK (until_state IN ('awaiting_review','completed','failed')),
 from_sequence bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 -- One live watch per user, conversation and task: registering again replaces
 -- it, so a repeated request still pings exactly once.
 UNIQUE (task_id, user_id, conversation_id)
);
CREATE INDEX agent_task_watchers_scan ON agent_task_watchers(task_id, created_at);
