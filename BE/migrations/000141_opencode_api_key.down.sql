-- Rolling back deletes saved OpenCode API keys; other sign-ins stay.
DELETE FROM agent_sign_ins WHERE provider = 'opencode';
ALTER TABLE agent_sign_ins
    DROP CONSTRAINT agent_sign_ins_provider_check,
    DROP CONSTRAINT agent_sign_ins_token_provider,
    ADD CONSTRAINT agent_sign_ins_provider_check CHECK (provider IN ('codex', 'claude-code')),
    ADD CONSTRAINT agent_sign_ins_token_provider CHECK (kind = 'credentials' OR provider = 'claude-code');
