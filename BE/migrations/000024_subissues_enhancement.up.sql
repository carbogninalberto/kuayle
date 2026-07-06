ALTER TABLE teams
    ADD COLUMN IF NOT EXISTS parent_auto_close_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sub_issue_auto_close_enabled BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE issues
    ADD CONSTRAINT issues_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id);

CREATE OR REPLACE FUNCTION prevent_issue_parent_cycle()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.parent_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.parent_id = NEW.id THEN
        RAISE EXCEPTION 'issue cannot be its own parent';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM issues parent
        WHERE parent.id = NEW.parent_id
          AND parent.workspace_id <> NEW.workspace_id
    ) THEN
        RAISE EXCEPTION 'issue parent must belong to the same workspace';
    END IF;

    IF EXISTS (
        WITH RECURSIVE ancestors AS (
            SELECT id, parent_id
            FROM issues
            WHERE id = NEW.parent_id

            UNION ALL

            SELECT i.id, i.parent_id
            FROM issues i
            INNER JOIN ancestors a ON i.id = a.parent_id
        )
        SELECT 1 FROM ancestors WHERE id = NEW.id
    ) THEN
        RAISE EXCEPTION 'issue parent cycle detected';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_issues_prevent_parent_cycle ON issues;
CREATE TRIGGER trg_issues_prevent_parent_cycle
    BEFORE INSERT OR UPDATE OF parent_id, workspace_id ON issues
    FOR EACH ROW
    EXECUTE FUNCTION prevent_issue_parent_cycle();
