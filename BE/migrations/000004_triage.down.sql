DROP INDEX IF EXISTS idx_issues_team_triaged;
ALTER TABLE issues DROP COLUMN IF EXISTS triaged;
ALTER TABLE teams DROP COLUMN IF EXISTS triage_enabled;
