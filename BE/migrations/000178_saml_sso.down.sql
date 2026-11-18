DELETE FROM workspace_sso_configs WHERE saml_metadata <> '';
ALTER TABLE workspace_sso_configs DROP COLUMN saml_metadata;
