CREATE TABLE telegram_settings (
 workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
 bot_id bigint UNIQUE NOT NULL CHECK(bot_id>0),
 bot_username text NOT NULL DEFAULT '',
 token_encrypted text NOT NULL,
 webhook_secret text NOT NULL,
 enabled boolean NOT NULL DEFAULT false,
 allowed_user_ids bigint[] NOT NULL DEFAULT '{}',
 status text NOT NULL DEFAULT 'saved',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE telegram_link_codes (
 workspace_id uuid NOT NULL REFERENCES telegram_settings(workspace_id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 telegram_user_id bigint NOT NULL CHECK(telegram_user_id>0),
 code_hash text NOT NULL UNIQUE,
 expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes',
 PRIMARY KEY(workspace_id,user_id)
);
CREATE TABLE telegram_links (
 workspace_id uuid NOT NULL REFERENCES telegram_settings(workspace_id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 telegram_user_id bigint NOT NULL CHECK(telegram_user_id>0),
 conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
 PRIMARY KEY(workspace_id,telegram_user_id),
 UNIQUE(workspace_id,user_id)
);
CREATE TABLE telegram_updates (
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 bot_id bigint NOT NULL,
 update_id bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,bot_id,update_id)
);
CREATE TABLE telegram_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES telegram_settings(workspace_id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 telegram_user_id bigint NOT NULL,
 conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,
 message_id uuid REFERENCES conversation_messages(id) ON DELETE CASCADE,
 text text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','sending','sent','uncertain','denied')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX telegram_delivery_queue ON telegram_deliveries(created_at) WHERE status='queued';
CREATE UNIQUE INDEX telegram_delivery_message ON telegram_deliveries(message_id,telegram_user_id) WHERE message_id IS NOT NULL;
CREATE INDEX telegram_delivery_recovery ON telegram_deliveries(updated_at) WHERE status='sending';
