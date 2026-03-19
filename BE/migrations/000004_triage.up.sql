-- Phase 6: Triage System
ALTER TABLE teams ADD COLUMN triage_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE issues ADD COLUMN triaged BOOLEAN NOT NULL DEFAULT true;

-- Existing issues are already triaged; new issues on triage-enabled teams will default to false via application logic
CREATE INDEX idx_issues_team_triaged ON issues(team_id) WHERE triaged = false;
