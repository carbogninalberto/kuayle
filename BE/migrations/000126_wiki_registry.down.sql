-- Rolling back keeps registry pages as ordinary pages and drops the settings.
DROP INDEX IF EXISTS wiki_pages_project_registry_key;
DROP INDEX IF EXISTS wiki_pages_team_registry_key;
ALTER TABLE wiki_pages DROP COLUMN IF EXISTS registry_date;
DROP TABLE IF EXISTS wiki_settings;
