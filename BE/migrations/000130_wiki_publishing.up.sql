-- Publishing a wiki page: off, internal (a read-only reader for members who
-- can already see the wiki) or public (anyone with the link). A public page
-- gets an ordinary shared link of scope 'wiki_page', so tokens, revocation,
-- rate limits and the privacy-mode rules are the ones issue shares use.
-- publish_children extends the publication to every page nested under it.
ALTER TABLE wiki_pages
    ADD COLUMN publish_mode VARCHAR(16) NOT NULL DEFAULT 'off' CHECK (publish_mode IN ('off', 'internal', 'public')),
    ADD COLUMN publish_children BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE shared_links DROP CONSTRAINT IF EXISTS shared_links_scope_check;
ALTER TABLE shared_links ADD CONSTRAINT shared_links_scope_check
    CHECK (scope IN ('workspace', 'team', 'project', 'view', 'wiki_page'));

-- A page has at most one public link at a time.
CREATE UNIQUE INDEX shared_links_wiki_page_idx ON shared_links (scope_id) WHERE scope = 'wiki_page';
