ALTER TABLE cli_connection_links DROP COLUMN IF EXISTS sso_workspace_id;
ALTER TABLE personal_access_tokens DROP COLUMN IF EXISTS sso_workspace_id;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS sso_workspace_id;
DROP TABLE IF EXISTS sso_login_states;
DROP TABLE IF EXISTS user_identities;
DROP TABLE IF EXISTS workspace_sso_configs;
