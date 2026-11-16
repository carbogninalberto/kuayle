-- Comments can be edited and soft-deleted. A deleted comment stays restorable
-- for the issue delete grace period; the purge job then removes it, and its
-- replies through the parent_id cascade. Readers filter deleted_at IS NULL.
ALTER TABLE comments
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN deleted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN edited_at TIMESTAMPTZ;

CREATE INDEX idx_comments_deleted_at ON comments(deleted_at) WHERE deleted_at IS NOT NULL;
