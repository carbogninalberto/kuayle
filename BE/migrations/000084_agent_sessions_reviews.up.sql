-- Agent sessions: every task attempt runs inside a tracked harness session.
-- An implementing task keeps one session across its follow-up attempts; a
-- requested review runs as its own review task with its own session.
ALTER TABLE agent_tasks ADD COLUMN kind text NOT NULL DEFAULT 'implement' CHECK (kind IN ('implement','review'));
-- A review task reviews the published pull request of another task. It never
-- validates, publishes or reaches review itself.
ALTER TABLE agent_tasks ADD COLUMN review_of_task_id uuid REFERENCES agent_tasks(id) ON DELETE CASCADE;
ALTER TABLE agent_tasks ADD CONSTRAINT agent_tasks_review_target CHECK ((kind='review')=(review_of_task_id IS NOT NULL));
CREATE INDEX agent_tasks_review_of ON agent_tasks(review_of_task_id,created_at) WHERE review_of_task_id IS NOT NULL;

CREATE TABLE agent_sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 task_id uuid NOT NULL UNIQUE REFERENCES agent_tasks(id) ON DELETE CASCADE,
 machine_id uuid,
 provider text NOT NULL,
 purpose text NOT NULL CHECK (purpose IN ('implement','review')),
 state text NOT NULL DEFAULT 'created' CHECK (state IN ('created','starting','active','waiting_input','suspended','completed','failed','cancelled')),
 -- Reported by the harness (Claude Code session ID assigned by Kuayle, Codex
 -- thread ID); the same safe form as dev_machine_agent_runs.native_session_id.
 native_session_id text CHECK (native_session_id ~ '^[A-Za-z0-9][A-Za-z0-9-]{0,127}$'),
 started_at timestamptz,
 ended_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_sessions_workspace ON agent_sessions(workspace_id,updated_at);
ALTER TABLE dev_machine_agent_runs ADD COLUMN session_id uuid REFERENCES agent_sessions(id) ON DELETE SET NULL;
CREATE INDEX dev_machine_agent_runs_session ON dev_machine_agent_runs(session_id) WHERE session_id IS NOT NULL;

-- Existing tasks get their implement session, derived from their state and
-- current attempt; their attempts are linked to it.
INSERT INTO agent_sessions(workspace_id,task_id,machine_id,provider,purpose,state,native_session_id,started_at,ended_at,created_at,updated_at)
SELECT t.workspace_id,t.id,t.machine_id,t.provider,'implement',
 CASE t.state WHEN 'queued' THEN CASE WHEN t.generation>1 THEN 'starting' ELSE 'created' END
  WHEN 'preparing' THEN 'starting' WHEN 'running' THEN 'active' WHEN 'waiting_input' THEN 'waiting_input'
  WHEN 'completed' THEN 'completed' WHEN 'failed' THEN 'failed' WHEN 'cancelled' THEN 'cancelled'
  ELSE 'suspended' END,
 (SELECT r.native_session_id FROM dev_machine_agent_runs r WHERE r.task_id=t.id AND r.native_session_id IS NOT NULL ORDER BY r.created_at DESC LIMIT 1),
 (SELECT min(r.started_at) FROM dev_machine_agent_runs r WHERE r.task_id=t.id),
 CASE WHEN t.state IN ('completed','failed','cancelled') THEN t.updated_at END,
 t.created_at,t.updated_at
FROM agent_tasks t;
UPDATE dev_machine_agent_runs r SET session_id=s.id FROM agent_sessions s WHERE s.task_id=r.task_id;

-- A requested review of a task's published pull request, pinned to the commit
-- Kuayle published. Findings are the reviewer's final output: bounded,
-- untrusted text. A pull request comment is posted only when requested.
CREATE TABLE agent_reviews (
 task_id uuid PRIMARY KEY REFERENCES agent_tasks(id) ON DELETE CASCADE,
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 reviewed_task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 requested_by uuid NOT NULL,
 provider text NOT NULL,
 commit_sha text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
 repository_full_name text NOT NULL,
 pull_request_number integer NOT NULL CHECK (pull_request_number>0),
 pull_request_url text NOT NULL,
 base_branch text NOT NULL DEFAULT '',
 focus text NOT NULL DEFAULT '' CHECK (octet_length(focus)<=4000),
 -- Increments only when an earlier review of the same commit and harness
 -- failed or was cancelled, so requests are otherwise idempotent.
 attempt integer NOT NULL DEFAULT 1 CHECK (attempt>0),
 post_to_pull_request boolean NOT NULL DEFAULT false,
 verdict text CHECK (verdict IN ('approve','request_changes','comment','unknown')),
 findings text CHECK (octet_length(findings)<=16384),
 comment_state text NOT NULL DEFAULT 'not_requested' CHECK (comment_state IN ('not_requested','pending','posted','failed')),
 comment_attempts integer NOT NULL DEFAULT 0 CHECK (comment_attempts>=0),
 comment_lease_expires_at timestamptz,
 completed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (reviewed_task_id,commit_sha,provider,attempt),
 CHECK (post_to_pull_request OR comment_state='not_requested')
);
CREATE INDEX agent_reviews_reviewed ON agent_reviews(reviewed_task_id,created_at);
CREATE INDEX agent_reviews_comments ON agent_reviews(comment_lease_expires_at) WHERE comment_state='pending';

-- Assistant approvals for requesting a review and for a follow-up on a task.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task'));
