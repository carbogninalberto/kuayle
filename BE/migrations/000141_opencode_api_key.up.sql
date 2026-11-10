-- A user can save an OpenCode API key next to their Claude Code token. It is
-- kept as kind 'token' without expires_at (API keys do not expire on a known
-- date); OpenCode has no credentials file to capture from a terminal.
ALTER TABLE agent_sign_ins
    DROP CONSTRAINT agent_sign_ins_provider_check,
    DROP CONSTRAINT agent_sign_ins_token_provider,
    ADD CONSTRAINT agent_sign_ins_provider_check CHECK (provider IN ('codex', 'claude-code', 'opencode')),
    ADD CONSTRAINT agent_sign_ins_token_provider CHECK (
        (kind = 'credentials' AND provider IN ('codex', 'claude-code'))
        OR (kind = 'token' AND provider IN ('claude-code', 'opencode'))
    );
