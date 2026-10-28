-- A Claude Code token from `claude setup-token` can be saved next to the
-- credentials file captured from the terminal. Runs prefer the token; its
-- expiry is Kuayle's estimate (one year after it was saved), never later than
-- the token's real expiry.
ALTER TABLE agent_sign_ins
    ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'credentials' CHECK (kind IN ('credentials', 'token')),
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD CONSTRAINT agent_sign_ins_token_provider CHECK (kind = 'credentials' OR provider = 'claude-code');
ALTER TABLE agent_sign_ins DROP CONSTRAINT agent_sign_ins_pkey;
ALTER TABLE agent_sign_ins ADD PRIMARY KEY (workspace_id, user_id, provider, kind);
