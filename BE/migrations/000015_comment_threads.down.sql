DROP INDEX IF EXISTS idx_comments_parent;
ALTER TABLE comments DROP COLUMN IF EXISTS resolved_at;
ALTER TABLE comments DROP COLUMN IF EXISTS parent_id;
