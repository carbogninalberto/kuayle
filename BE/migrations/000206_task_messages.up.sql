-- The room of a pipeline task (KUA-252): one ordered log of what the task's
-- creator, its roles and Kuayle post. Text from agents is untrusted and only
-- shown. A refused post is kept, with why, for its sender; it is never
-- delivered as a message.
CREATE TABLE task_messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 -- The root (implementing) task.
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 -- Allocated per task under the task's advisory lock.
 seq bigint NOT NULL CHECK (seq > 0),
 author_kind text NOT NULL CHECK (author_kind IN ('human','member','kuayle')),
 author_role text CHECK (char_length(author_role) BETWEEN 1 AND 32),
 author_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
 -- A row of task_members; its foreign key comes with that table.
 author_member_id uuid,
 run_id uuid,
 -- Empty means everyone.
 to_roles text[] NOT NULL DEFAULT '{}' CHECK (cardinality(to_roles) <= 16),
 kind text NOT NULL CHECK (kind IN ('say','ask','phase','verdict','request','waiver','grant','fact','summary')),
 body text NOT NULL CHECK (octet_length(body) <= 16384),
 data jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object' AND octet_length(data::text) <= 16384),
 reply_to bigint CHECK (reply_to > 0),
 dedupe_key text CHECK (char_length(dedupe_key) BETWEEN 1 AND 128),
 reject_reason text CHECK (char_length(reject_reason) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (task_id, seq),
 UNIQUE (task_id, dedupe_key),
 CHECK ((author_kind = 'member') = (author_role IS NOT NULL)),
 CHECK (author_kind = 'human' OR author_user_id IS NULL),
 CHECK (author_kind = 'member' OR (author_member_id IS NULL AND reject_reason IS NULL))
);
