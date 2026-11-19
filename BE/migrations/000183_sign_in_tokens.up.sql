CREATE TABLE sign_in_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    token_hash text NOT NULL UNIQUE CHECK (length(token_hash) = 64),
    purpose text NOT NULL CHECK (purpose = 'telegram'),
    expires_at timestamptz NOT NULL DEFAULT (now() + interval '10 minutes'),
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at <= created_at + interval '10 minutes'),
    FOREIGN KEY (workspace_id, user_id) REFERENCES telegram_links (workspace_id, user_id) ON DELETE CASCADE
);

CREATE INDEX sign_in_tokens_expiry ON sign_in_tokens (expires_at);
CREATE INDEX sign_in_tokens_binding ON sign_in_tokens (workspace_id, user_id);
