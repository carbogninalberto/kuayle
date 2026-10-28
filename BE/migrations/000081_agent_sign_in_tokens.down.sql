-- Rolling back deletes saved Claude Code tokens; captured sign-ins stay.
DELETE FROM agent_sign_ins WHERE kind = 'token';
ALTER TABLE agent_sign_ins DROP CONSTRAINT agent_sign_ins_pkey;
ALTER TABLE agent_sign_ins ADD PRIMARY KEY (workspace_id, user_id, provider);
ALTER TABLE agent_sign_ins DROP CONSTRAINT agent_sign_ins_token_provider, DROP COLUMN expires_at, DROP COLUMN kind;
