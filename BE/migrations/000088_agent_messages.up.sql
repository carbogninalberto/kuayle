-- Agent messaging: agents of related tasks exchange bounded, untrusted text.
-- Every message on both channels is recorded here (the "navigator" registry),
-- including refused ones, so people can debug and clear the traffic.
CREATE TABLE agent_messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 from_task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 from_issue_id uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
 -- The native attempt whose worktree held the outbox file, when known.
 from_run_id uuid,
 -- What the agent wrote as the recipient; resolved only to a related issue.
 to_ref text NOT NULL CHECK (char_length(to_ref) BETWEEN 1 AND 64),
 to_issue_id uuid REFERENCES issues(id) ON DELETE CASCADE,
 -- The recipient issue's newest implementing task when sent, then the task
 -- whose attempt received it.
 to_task_id uuid REFERENCES agent_tasks(id) ON DELETE SET NULL,
 channel text NOT NULL CHECK (channel IN ('internal','comment')),
 body text NOT NULL CHECK (octet_length(body) <= 16384),
 state text NOT NULL CHECK (state IN ('queued','held','delivered','dropped','rejected')),
 reject_reason text CHECK (char_length(reject_reason) <= 200),
 -- One record per outbox file content, so a file taken twice is kept once.
 dedupe_key text NOT NULL CHECK (char_length(dedupe_key) <= 128),
 held_by uuid REFERENCES users(id) ON DELETE SET NULL,
 held_at timestamptz,
 released_by uuid REFERENCES users(id) ON DELETE SET NULL,
 released_at timestamptz,
 dropped_by uuid REFERENCES users(id) ON DELETE SET NULL,
 dropped_at timestamptz,
 delivered_run_id uuid,
 delivered_at timestamptz,
 comment_state text NOT NULL DEFAULT 'none' CHECK (comment_state IN ('none','pending','posted','failed')),
 comment_id uuid REFERENCES comments(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (from_task_id, dedupe_key),
 CHECK (state = 'rejected' OR to_issue_id IS NOT NULL),
 CHECK (state <> 'rejected' OR reject_reason IS NOT NULL),
 CHECK (state <> 'delivered' OR (delivered_run_id IS NOT NULL AND delivered_at IS NOT NULL)),
 CHECK (channel = 'comment' OR comment_state = 'none')
);
CREATE INDEX agent_messages_workspace ON agent_messages(workspace_id, created_at DESC, id);
CREATE INDEX agent_messages_inbox ON agent_messages(to_issue_id, created_at) WHERE state = 'queued';
CREATE INDEX agent_messages_from_issue ON agent_messages(from_issue_id, created_at DESC);
CREATE INDEX agent_messages_to_issue ON agent_messages(to_issue_id, created_at DESC);
CREATE INDEX agent_messages_sender_rate ON agent_messages(from_task_id, created_at);
CREATE INDEX agent_messages_comments ON agent_messages(created_at) WHERE comment_state = 'pending';

-- A held conversation: new and queued messages between the two issues' agents
-- wait for a person instead of being delivered. issue_a < issue_b.
CREATE TABLE agent_message_holds (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 issue_a uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
 issue_b uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
 created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (issue_a < issue_b),
 UNIQUE (issue_a, issue_b)
);

-- Audit of every human action on messages and held conversations.
CREATE TABLE agent_message_actions (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 message_id uuid REFERENCES agent_messages(id) ON DELETE CASCADE,
 issue_a uuid,
 issue_b uuid,
 action text NOT NULL CHECK (action IN ('hold','release','drop','hold_conversation','release_conversation')),
 actor_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (message_id IS NOT NULL OR (issue_a IS NOT NULL AND issue_b IS NOT NULL))
);
CREATE INDEX agent_message_actions_message ON agent_message_actions(message_id, id) WHERE message_id IS NOT NULL;
CREATE INDEX agent_message_actions_workspace ON agent_message_actions(workspace_id, created_at DESC);

-- A comment Kuayle posted for an agent's message: it is shown as the agent's,
-- while user_id stays the sending task's creator, who is accountable for it.
ALTER TABLE comments ADD COLUMN agent_task_id uuid REFERENCES agent_tasks(id) ON DELETE SET NULL;
