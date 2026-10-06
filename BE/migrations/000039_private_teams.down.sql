-- Never turn a downgrade into an implicit disclosure. Administrators must
-- explicitly make every private team public using the privacy-aware application
-- before removing this schema. Do not run an older binary against private data.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM teams WHERE is_private) THEN
        RAISE EXCEPTION 'Cannot remove private-team protection while private teams exist';
    END IF;
END $$;
DROP INDEX idx_teams_private_workspace;
DROP INDEX idx_team_members_user_team;
ALTER TABLE teams DROP COLUMN is_private;
