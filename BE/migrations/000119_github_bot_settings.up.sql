-- A workspace's Kuayle GitHub bot: the name and profile picture Kuayle signs
-- its GitHub comments with and shows in settings, and which abilities the bot
-- may use. No row means the defaults: name "Kuayle", no picture, every
-- existing ability on and the mention-triggered review off. GitHub itself
-- always shows the GitHub App account as the author.
CREATE TABLE workspace_github_bot_settings (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    display_name TEXT CHECK (display_name IS NULL OR char_length(display_name) BETWEEN 1 AND 64),
    -- An external https image, validated when it was saved.
    avatar_url TEXT CHECK (avatar_url IS NULL OR (char_length(avatar_url) <= 2048 AND avatar_url LIKE 'https://%')),
    -- An uploaded image in the application's file storage.
    avatar_file TEXT CHECK (avatar_file IS NULL OR avatar_file ~ '^[0-9a-f-]{36}\.(png|jpg|webp|gif)$'),
    -- Explicit choices only, e.g. {"merge_pull_requests": false}; a missing
    -- ability keeps its default.
    abilities JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(abilities) = 'object'),
    -- Harness and model for reviews the bot starts when mentioned; empty
    -- means the defaults a review requested in chat uses.
    mention_harness TEXT NOT NULL DEFAULT '' CHECK (mention_harness IN ('', 'claude-code', 'codex', 'opencode')),
    mention_model TEXT NOT NULL DEFAULT '' CHECK (char_length(mention_model) <= 128),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (avatar_url IS NULL OR avatar_file IS NULL)
);
