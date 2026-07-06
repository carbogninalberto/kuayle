DROP TRIGGER IF EXISTS trg_issues_prevent_parent_cycle ON issues;
DROP FUNCTION IF EXISTS prevent_issue_parent_cycle();

ALTER TABLE issues
    DROP CONSTRAINT IF EXISTS issues_parent_not_self;

ALTER TABLE teams
    DROP COLUMN IF EXISTS sub_issue_auto_close_enabled,
    DROP COLUMN IF EXISTS parent_auto_close_enabled;
