DROP INDEX IF EXISTS idx_issues_status_id;
DROP INDEX IF EXISTS idx_team_statuses_team;
ALTER TABLE issues DROP COLUMN IF EXISTS status_id;
DROP TABLE IF EXISTS team_statuses;
