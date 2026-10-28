CREATE TABLE agent_tasks (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 conversation_id uuid,
 -- Immutable audit identities survive issue/user deletion so workers can cancel
 -- outstanding execution. They never grant access without the live conversation.
 creator_id uuid NOT NULL,
 issue_id uuid NOT NULL,
 team_id uuid NOT NULL,
 -- Delegation snapshot prevents automation overwriting later issue edits.
 issue_status_id uuid,
 issue_updated_at timestamptz,
 request_id uuid NOT NULL,
 prompt text NOT NULL CHECK (octet_length(prompt) BETWEEN 1 AND 32768),
 provider text NOT NULL,
 model text NOT NULL DEFAULT '',
 thinking text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','preparing','running','waiting_input','validating','publishing','awaiting_review','blocked','completed','failed','cancelling','cancelled')),
 reason text NOT NULL DEFAULT '' CHECK (octet_length(reason)<=2048),
 generation bigint NOT NULL DEFAULT 1 CHECK (generation>0),
 machine_id uuid,
 -- Reserved IDs are allocated before any runtime side effects. They are not
 -- foreign keys because creation may not have happened yet or may have failed.
 reserved_machine_id uuid NOT NULL DEFAULT gen_random_uuid(),
 checkout_id uuid NOT NULL DEFAULT gen_random_uuid(),
 run_id uuid NOT NULL DEFAULT gen_random_uuid(),
 failure_count integer NOT NULL DEFAULT 0 CHECK(failure_count BETWEEN 0 AND 5),
 failure_pending boolean NOT NULL DEFAULT false,
 run_cursor bigint NOT NULL DEFAULT 0,
 lease_token uuid,
 lease_expires_at timestamptz,
 next_poll_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(conversation_id,workspace_id) REFERENCES conversations(id,workspace_id) ON DELETE SET NULL (conversation_id),
 UNIQUE(workspace_id,creator_id,request_id),
 CHECK ((lease_token IS NULL)=(lease_expires_at IS NULL))
);
CREATE INDEX agent_tasks_conversation ON agent_tasks(conversation_id,created_at,id);
CREATE INDEX agent_tasks_pending ON agent_tasks(next_poll_at,created_at) WHERE state NOT IN ('completed','cancelled','failed','blocked','awaiting_review');
CREATE TABLE agent_task_transitions (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 from_state text NOT NULL,
 to_state text NOT NULL,
 actor_id uuid,
 reason text NOT NULL CHECK (octet_length(reason)<=2048),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_task_history ON agent_task_transitions(task_id,sequence);

-- Manual issue checkouts retain their original scope. Each task owns a separate
-- worktree, even when another task addresses the same issue on the same machine.
ALTER TABLE dev_machine_checkouts ADD COLUMN scope_key text NOT NULL DEFAULT '';
ALTER TABLE dev_machine_checkouts DROP CONSTRAINT dev_machine_checkouts_machine_id_issue_id_key;
ALTER TABLE dev_machine_checkouts ADD CONSTRAINT dev_machine_checkout_scope UNIQUE(machine_id,issue_id,scope_key);

-- Retain attempt ownership even if the task moves to a successor attempt. A
-- missing task denies execution rather than converting an attempt to manual.
ALTER TABLE dev_machine_agent_runs ADD COLUMN task_id uuid;
CREATE INDEX dev_machine_agent_runs_task ON dev_machine_agent_runs(task_id,created_at,id) WHERE task_id IS NOT NULL;

-- Immutable encrypted handoff for native recovery and output redaction.
ALTER TABLE dev_machine_agent_runs ADD COLUMN encrypted_output_secrets text;

-- The first native Docker log timestamp identifies a retained stream prefix.
-- A rotated prefix must not collide with earlier logical line numbers.
ALTER TABLE dev_machine_log_chunks ADD COLUMN source_key text NOT NULL DEFAULT '';

CREATE TABLE agent_task_retries (
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 request_id uuid NOT NULL,
 expected_generation bigint NOT NULL CHECK(expected_generation>0),
 prompt text NOT NULL CHECK(octet_length(prompt) BETWEEN 1 AND 32768),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(task_id,request_id)
);
