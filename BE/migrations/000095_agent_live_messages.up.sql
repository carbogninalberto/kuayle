-- The live channel of a task: messages from the person who owns the task (or
-- the orchestrator acting for them) to its running agent, and the agent's
-- messages to the orchestrator. Agent text is untrusted and only shown.
CREATE TABLE agent_live_messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 direction text NOT NULL CHECK (direction IN ('to_agent','from_agent')),
 -- Orders messages to the agent and names their inbox files (<seq>.json).
 seq bigint CHECK (seq > 0),
 sender text NOT NULL CHECK (sender IN ('user','orchestrator','agent')),
 -- The person who sent it, or on whose behalf the orchestrator did.
 author_id uuid REFERENCES users(id) ON DELETE SET NULL,
 kind text NOT NULL DEFAULT 'message' CHECK (kind IN ('message','question')),
 body text NOT NULL CHECK (octet_length(body) <= 16384),
 state text NOT NULL CHECK (state IN ('queued','delivered','received','rejected')),
 delivered_via text CHECK (delivered_via IN ('inbox','prompt')),
 -- The attempt that received a message to the agent, or sent one from it.
 run_id uuid,
 reject_reason text CHECK (char_length(reject_reason) <= 200),
 -- One record per outbox file content, so a file taken twice is kept once.
 dedupe_key text CHECK (char_length(dedupe_key) <= 128),
 created_at timestamptz NOT NULL DEFAULT now(),
 delivered_at timestamptz,
 UNIQUE (task_id, seq),
 UNIQUE (task_id, dedupe_key),
 CHECK ((direction = 'to_agent') = (seq IS NOT NULL)),
 CHECK ((direction = 'to_agent') = (sender <> 'agent')),
 CHECK (direction = 'from_agent' OR (state IN ('queued','delivered') AND dedupe_key IS NULL)),
 CHECK (direction = 'to_agent' OR (state IN ('received','rejected') AND dedupe_key IS NOT NULL AND delivered_via IS NULL)),
 CHECK (state <> 'delivered' OR (delivered_via IS NOT NULL AND run_id IS NOT NULL AND delivered_at IS NOT NULL)),
 CHECK (state = 'delivered' OR delivered_via IS NULL),
 CHECK (state <> 'rejected' OR reject_reason IS NOT NULL)
);
CREATE INDEX agent_live_messages_task ON agent_live_messages(task_id, created_at, id);
CREATE INDEX agent_live_messages_pending ON agent_live_messages(task_id, seq) WHERE state = 'queued';
CREATE INDEX agent_live_messages_workspace ON agent_live_messages(workspace_id, created_at DESC);
