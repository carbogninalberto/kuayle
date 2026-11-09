-- Wiki pages form a tree. A folder is a page of kind 'folder' (it can still
-- hold text); any page may hold children, so nesting needs one parent column
-- rather than a separate folder table. Moves are validated by the
-- repository (same scope, no cycles, depth limit) under row locks; the
-- trigger keeps a deleted page's children in place by lifting them to its
-- parent instead of orphaning or deleting them. Registry day pages stay out
-- of the tree.
ALTER TABLE wiki_pages
    ADD COLUMN parent_id UUID REFERENCES wiki_pages(id) ON DELETE SET NULL,
    ADD COLUMN position INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'page' CHECK (kind IN ('page', 'folder')),
    ADD CONSTRAINT wiki_pages_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id);

CREATE INDEX wiki_pages_parent_idx ON wiki_pages (parent_id, position) WHERE parent_id IS NOT NULL;

-- Existing pages keep the order the list showed: most recently updated first.
UPDATE wiki_pages SET position = ordered.position
FROM (
    SELECT id, (ROW_NUMBER() OVER (PARTITION BY team_id, project_id ORDER BY updated_at DESC, id) - 1) AS position
    FROM wiki_pages
) ordered
WHERE ordered.id = wiki_pages.id;

CREATE FUNCTION wiki_pages_lift_children() RETURNS trigger AS $$
BEGIN
    UPDATE wiki_pages SET parent_id = OLD.parent_id WHERE parent_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER wiki_pages_lift_children BEFORE DELETE ON wiki_pages
    FOR EACH ROW EXECUTE FUNCTION wiki_pages_lift_children();
