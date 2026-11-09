-- Wiki page versions record what each save changed, so a page's history can
-- be read like an issue's. Autosave writes often: consecutive saves by the
-- same person within a few minutes extend one revision instead of adding new
-- rows (see WikiRepository.Update). Access is never checked on this table
-- alone: reads join wiki_pages and apply its scope visibility predicate.
CREATE TABLE wiki_page_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    old_title VARCHAR(200) NOT NULL,
    new_title VARCHAR(200) NOT NULL,
    old_content TEXT NOT NULL DEFAULT '',
    new_content TEXT NOT NULL DEFAULT '',
    edited_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX wiki_page_versions_page_idx ON wiki_page_versions (page_id, updated_at DESC);
