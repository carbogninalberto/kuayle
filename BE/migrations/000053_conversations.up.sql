CREATE TABLE conversations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 creator_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(id,workspace_id)
);
CREATE INDEX conversations_history ON conversations(workspace_id,creator_id,updated_at DESC,id);
CREATE TABLE conversation_messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
 request_id uuid NOT NULL,
 parent_id uuid,
 role text NOT NULL CHECK (role IN ('user','assistant')),
 content text NOT NULL DEFAULT '' CHECK (octet_length(content)<=65536),
 status text NOT NULL CHECK (status IN ('queued','streaming','completed','failed','interrupted','cancelled')),
 provider text NOT NULL DEFAULT '',
 model text NOT NULL DEFAULT '',
 lease_token uuid,
 lease_expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conversation_id,request_id,role),
 UNIQUE(id,conversation_id),
 FOREIGN KEY(parent_id,conversation_id) REFERENCES conversation_messages(id,conversation_id),
 CHECK ((role='user' AND parent_id IS NULL AND status='completed') OR (role='assistant' AND parent_id IS NOT NULL)),
 CHECK ((status='streaming') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE UNIQUE INDEX conversation_one_generation ON conversation_messages(conversation_id) WHERE status IN ('queued','streaming');
CREATE INDEX conversation_message_history ON conversation_messages(conversation_id,sequence DESC);
CREATE INDEX conversation_queued ON conversation_messages(sequence) WHERE status='queued';
CREATE TABLE conversation_events (
 cursor bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 type text NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversation_event_replay ON conversation_events(conversation_id,cursor);
