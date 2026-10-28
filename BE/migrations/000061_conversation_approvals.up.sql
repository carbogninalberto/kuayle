-- Human approvals requested by assistant tools. Only the bound actor may resolve
-- one; `approving` claims the decision so the effect runs at most once and an
-- interrupted approval can be resumed idempotently through `request_id`.
CREATE TABLE conversation_approvals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 tool_call_id uuid NOT NULL UNIQUE REFERENCES conversation_tool_calls(id) ON DELETE CASCADE,
 action text NOT NULL CHECK (action IN ('delegate_issue')),
 actor_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 issue_id uuid NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
 payload jsonb NOT NULL CHECK (octet_length(payload::text) <= 40960),
 status text NOT NULL CHECK (status IN ('pending','approving','approved','denied','expired','failed')),
 request_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 task_id uuid REFERENCES agent_tasks(id) ON DELETE SET NULL,
 error text CHECK (octet_length(error) <= 2048),
 expires_at timestamptz NOT NULL,
 resolved_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((status IN ('pending','approving')) = (resolved_at IS NULL))
);
CREATE INDEX conversation_approvals_conversation ON conversation_approvals(conversation_id, created_at);
