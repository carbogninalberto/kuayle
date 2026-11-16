-- Rolling back cannot hide deleted comments any more, so it purges them.
DELETE FROM comments WHERE deleted_at IS NOT NULL;
DROP INDEX IF EXISTS idx_comments_deleted_at;
ALTER TABLE comments
    DROP COLUMN IF EXISTS edited_at,
    DROP COLUMN IF EXISTS deleted_by,
    DROP COLUMN IF EXISTS deleted_at;
