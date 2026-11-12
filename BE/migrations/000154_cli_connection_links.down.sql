ALTER TABLE personal_access_tokens DROP COLUMN connection_link_id;
DROP INDEX IF EXISTS idx_cli_connection_links_user;
DROP TABLE cli_connection_links;
