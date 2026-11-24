-- Members of a pipeline task and their verdicts (KUA-252). A member is a role
-- of the pipeline working on one task; its turns are generations of an agent
-- task: the root (implementing) task for the role that writes, a review task
-- of the root for every other role. A verdict is a member's review of one
-- commit, posted in the room.

-- Composite keys, so that every reference below stays in its workspace.
ALTER TABLE agent_tasks ADD CONSTRAINT agent_tasks_id_workspace UNIQUE (id, workspace_id);
ALTER TABLE agent_profiles ADD CONSTRAINT agent_profiles_id_workspace UNIQUE (id, workspace_id);

CREATE TABLE task_members (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 -- The root (implementing) task.
 task_id uuid NOT NULL,
 role text NOT NULL CHECK (role ~ '^[a-z][a-z0-9_-]{0,31}$'),
 -- Pinned when the member is created: a later version of the pipeline or
 -- of the profile does not change who plays the role on this task.
 profile_id uuid NOT NULL,
 -- The task whose generations are the member's turns; NULL for the role
 -- that writes, whose turns are the root task's.
 member_task_id uuid UNIQUE,
 -- The room cursor: posts up to it were delivered.
 last_read_seq bigint NOT NULL DEFAULT 0 CHECK (last_read_seq >= 0),
 state text NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','queued','running')),
 turns integer NOT NULL DEFAULT 0 CHECK (turns >= 0),
 no_progress integer NOT NULL DEFAULT 0 CHECK (no_progress >= 0),
 -- The published commit the current (or last) turn reads, and whether that
 -- turn must end with a verdict for it.
 pinned_commit text CHECK (pinned_commit ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
 verdict_asked boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (task_id, role),
 UNIQUE (id, task_id),
 CHECK (member_task_id IS DISTINCT FROM task_id),
 CHECK (NOT verdict_asked OR pinned_commit IS NOT NULL),
 FOREIGN KEY (task_id, workspace_id) REFERENCES agent_tasks(id, workspace_id) ON DELETE CASCADE,
 FOREIGN KEY (member_task_id, workspace_id) REFERENCES agent_tasks(id, workspace_id) ON DELETE CASCADE,
 FOREIGN KEY (profile_id, workspace_id) REFERENCES agent_profiles(id, workspace_id)
);

-- A member's post names the member of the same task.
ALTER TABLE task_messages ADD CONSTRAINT task_messages_author_member
 FOREIGN KEY (author_member_id, task_id) REFERENCES task_members(id, task_id) ON DELETE SET NULL (author_member_id);

-- A verdict is the room post that gave it, for the full commit it names.
-- agent_reviews stays for reviews requested through the assistant.
CREATE TABLE task_verdicts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL,
 task_id uuid NOT NULL,
 role text NOT NULL,
 commit_sha text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
 verdict text NOT NULL CHECK (verdict IN ('approve','request_changes','comment')),
 findings text NOT NULL DEFAULT '' CHECK (octet_length(findings) <= 16384),
 run_id uuid,
 seq bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (task_id, seq),
 FOREIGN KEY (task_id, workspace_id) REFERENCES agent_tasks(id, workspace_id) ON DELETE CASCADE,
 FOREIGN KEY (task_id, role) REFERENCES task_members(task_id, role) ON DELETE CASCADE,
 FOREIGN KEY (task_id, seq) REFERENCES task_messages(task_id, seq) ON DELETE CASCADE
);
CREATE INDEX task_verdicts_head ON task_verdicts (task_id, commit_sha, role, seq DESC);
CREATE INDEX task_verdicts_run ON task_verdicts (run_id) WHERE run_id IS NOT NULL;
