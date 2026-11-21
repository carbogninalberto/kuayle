CREATE TABLE slack_oauth_states (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    state uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, user_id),
    FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_members ON DELETE CASCADE
);

CREATE TABLE slack_installs (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL UNIQUE REFERENCES workspaces ON DELETE CASCADE,
    team_id text NOT NULL UNIQUE,
    name text NOT NULL,
    bot_token text NOT NULL,
    scope text NOT NULL,
    installer_id uuid REFERENCES users ON DELETE SET NULL
);

CREATE TABLE slack_bindings (
    workspace_id uuid NOT NULL REFERENCES slack_installs(workspace_id) ON DELETE CASCADE,
    team_id uuid,
    project_id uuid,
    channel_id text NOT NULL,
    CHECK (num_nonnulls(team_id, project_id) = 1),
    FOREIGN KEY (workspace_id, team_id) REFERENCES teams(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, project_id) REFERENCES projects(workspace_id, id) ON DELETE CASCADE,
    UNIQUE NULLS NOT DISTINCT (workspace_id, team_id, project_id)
);

CREATE TABLE slack_threads (
    workspace_id uuid NOT NULL REFERENCES slack_installs(workspace_id) ON DELETE CASCADE,
    channel_id text NOT NULL,
    thread_ts text NOT NULL,
    issue_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, channel_id, thread_ts),
    FOREIGN KEY (workspace_id, issue_id) REFERENCES issue_records(workspace_id, id) ON DELETE CASCADE
);

CREATE TABLE slack_identities (
    workspace_id uuid NOT NULL REFERENCES slack_installs(workspace_id) ON DELETE CASCADE,
    slack_user_id text NOT NULL,
    user_id uuid NOT NULL,
    PRIMARY KEY (workspace_id, slack_user_id),
    UNIQUE (workspace_id, user_id),
    FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_members ON DELETE CASCADE
);

-- Only app_uninstalled is subscribed. Its event ID suffices to check the
-- current bot token asynchronously; Slack message bodies never enter storage.
CREATE TABLE slack_uninstall_events (
    event_id text PRIMARY KEY,
    install_id uuid NOT NULL REFERENCES slack_installs ON DELETE CASCADE,
    next_attempt timestamptz DEFAULT now()
);
CREATE INDEX slack_uninstall_pending ON slack_uninstall_events(next_attempt) WHERE next_attempt IS NOT NULL;
