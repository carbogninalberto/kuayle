ALTER TABLE comments ADD COLUMN parent_id UUID REFERENCES comments(id) ON DELETE CASCADE;
ALTER TABLE comments ADD COLUMN resolved_at TIMESTAMPTZ;
CREATE INDEX idx_comments_parent ON comments(parent_id);
