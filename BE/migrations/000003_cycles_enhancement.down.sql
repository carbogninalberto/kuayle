DROP INDEX IF EXISTS idx_cycles_team_status;
ALTER TABLE cycles DROP COLUMN IF EXISTS completed_at;
ALTER TABLE cycles DROP COLUMN IF EXISTS description;
ALTER TABLE cycles DROP COLUMN IF EXISTS status;
