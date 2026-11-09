DROP TRIGGER IF EXISTS wiki_pages_lift_children ON wiki_pages;
DROP FUNCTION IF EXISTS wiki_pages_lift_children();
DROP INDEX IF EXISTS wiki_pages_parent_idx;
ALTER TABLE wiki_pages
    DROP CONSTRAINT IF EXISTS wiki_pages_not_own_parent,
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS position,
    DROP COLUMN IF EXISTS parent_id;
