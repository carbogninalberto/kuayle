-- Role memory (KUA-282): a lesson may be for one role of the team
-- workflows' rosters (task_members.role). Such a lesson reaches only the
-- members of pipeline tasks that play that role, alongside the lessons
-- without a role; every other task keeps receiving only the lessons without
-- a role. A member's outbox lesson is proposed for its role, and an owner or
-- admin decides it as any other proposal.
ALTER TABLE workspace_lessons ADD COLUMN role text CHECK (role ~ '^[a-z][a-z0-9_-]{0,31}$');

-- The same subject is proposed once per repository, team scope and role.
DROP INDEX workspace_lessons_dedupe;
CREATE UNIQUE INDEX workspace_lessons_dedupe ON workspace_lessons
    (workspace_id, repository_key, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(role, ''), dedupe_key);
