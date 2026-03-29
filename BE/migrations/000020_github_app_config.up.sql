CREATE TABLE github_app_configs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
    app_id            BIGINT NOT NULL,
    app_slug          VARCHAR(255),
    client_id         VARCHAR(255) NOT NULL,
    client_secret     TEXT NOT NULL,
    private_key       TEXT NOT NULL,
    webhook_secret    TEXT NOT NULL,
    html_url          VARCHAR(512),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
