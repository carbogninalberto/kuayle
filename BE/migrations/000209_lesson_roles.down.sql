-- Without the column a role's lessons would reach every task, so they are
-- deleted, with their audit trail.
DELETE FROM workspace_lessons WHERE role IS NOT NULL;
DROP INDEX workspace_lessons_dedupe;
ALTER TABLE workspace_lessons DROP COLUMN role;
CREATE UNIQUE INDEX workspace_lessons_dedupe ON workspace_lessons
    (workspace_id, repository_key, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid), dedupe_key);
