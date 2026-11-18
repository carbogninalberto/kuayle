-- Named migration actions a workspace owner, admin or the machine's creator
-- configures on a Dev Machine (KUA-181), so the assistant can run a database
-- migration by name while the connection secret stays out of the model's
-- reach: the secret is stored encrypted, injected into the run container only
-- at run time and redacted from the stored output. A migration run is a
-- command run with a flag and the action's name, so it is queued, stored, read
-- and reported like any other command run.
CREATE TABLE dev_machine_migration_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    machine_id UUID NOT NULL REFERENCES dev_machines(id) ON DELETE CASCADE,
    -- The name the model asks for; one line, unique per machine.
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100 AND name = btrim(name)),
    description TEXT NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    -- The admin-configured command. It reaches the secret only as the
    -- $KUAYLE_MIGRATION_SECRET variable; the secret itself is never part of it.
    command TEXT NOT NULL CHECK (octet_length(command) BETWEEN 1 AND 4000),
    -- The encrypted connection secret, never returned by any API.
    encrypted_secret TEXT NOT NULL CHECK (octet_length(encrypted_secret) BETWEEN 1 AND 65536),
    encryption_key_version INT NOT NULL DEFAULT 1,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX dev_machine_migration_actions_name ON dev_machine_migration_actions (machine_id, lower(name));

-- A migration run is a command run: the stored command is the action's
-- admin-configured one, and action_name names the action for the transcript.
ALTER TABLE assistant_command_runs
    ADD COLUMN migration BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN action_name TEXT NOT NULL DEFAULT '' CHECK (octet_length(action_name) <= 100);
ALTER TABLE assistant_command_runs
    ADD CONSTRAINT assistant_command_runs_migration_check CHECK (NOT migration OR (action_name <> '' AND NOT write));

-- run_migration asks for approval and belongs to no issue, like the other
-- command tools.
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_action_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_action_check CHECK (action IN ('delegate_issue', 'merge_pull_request', 'request_agent_review', 'follow_up_task', 'accept_agent_work', 'start_preview', 'expose_port', 'run_check', 'run_custom_tool', 'upgrade_instance', 'reply_to_review_comment', 'resolve_review_thread', 'update_pull_request_branch', 'mark_pull_request_ready', 'create_release', 'ship_pull_request', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'message_task', 'start_temporary_machine', 'run_command', 'run_command_write', 'run_migration'));
ALTER TABLE conversation_approvals DROP CONSTRAINT conversation_approvals_issue_check;
ALTER TABLE conversation_approvals ADD CONSTRAINT conversation_approvals_issue_check CHECK (issue_id IS NOT NULL OR action IN ('upgrade_instance', 'create_release', 'delete_wiki_page', 'restore_wiki_page_version', 'disable_wiki_registry', 'share_wiki_page', 'start_temporary_machine', 'run_command', 'run_command_write', 'run_migration'));
