-- Drops agent assignments, team policies and identities; tasks keep running
-- but lose the link to their agent profile.
ALTER TABLE issue_history DROP COLUMN IF EXISTS agent_profile_id;
DROP TABLE IF EXISTS issue_agent_assignees;
DROP INDEX IF EXISTS agent_tasks_agent_profile;
ALTER TABLE agent_tasks DROP COLUMN IF EXISTS agent_profile_id;
DROP TABLE IF EXISTS agent_team_policies;
DROP TABLE IF EXISTS agent_identities;
