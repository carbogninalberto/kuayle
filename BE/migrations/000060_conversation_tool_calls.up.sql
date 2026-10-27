-- Assistant tool invocations. A call is recorded as running before it executes,
-- so an interrupted attempt leaves an explicit unknown outcome, never a rerun.
CREATE TABLE conversation_tool_calls (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 message_id uuid NOT NULL,
 sequence int NOT NULL CHECK (sequence >= 0),
 call_id text NOT NULL CHECK (char_length(call_id) BETWEEN 1 AND 128),
 name text NOT NULL CHECK (name ~ '^[a-z_]{1,64}$'),
 input jsonb NOT NULL,
 status text NOT NULL CHECK (status IN ('running','completed','failed')),
 output jsonb,
 error text CHECK (octet_length(error) <= 2048),
 issue_id uuid REFERENCES issues(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz,
 UNIQUE(message_id, sequence),
 FOREIGN KEY(message_id, conversation_id) REFERENCES conversation_messages(id, conversation_id) ON DELETE CASCADE,
 CHECK (octet_length(input::text) <= 16384 AND (output IS NULL OR octet_length(output::text) <= 32768)),
 CHECK ((status = 'running') = (completed_at IS NULL))
);
CREATE INDEX conversation_tool_calls_message ON conversation_tool_calls(message_id, sequence);
