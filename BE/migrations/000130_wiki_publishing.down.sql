DROP INDEX IF EXISTS shared_links_wiki_page_idx;
DELETE FROM shared_links WHERE scope = 'wiki_page';
ALTER TABLE shared_links DROP CONSTRAINT IF EXISTS shared_links_scope_check;
ALTER TABLE shared_links ADD CONSTRAINT shared_links_scope_check
    CHECK (scope IN ('workspace', 'team', 'project', 'view'));
ALTER TABLE wiki_pages DROP COLUMN IF EXISTS publish_children, DROP COLUMN IF EXISTS publish_mode;
