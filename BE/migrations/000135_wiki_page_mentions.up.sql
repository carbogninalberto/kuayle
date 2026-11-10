-- Index of the wiki pages mentioned in issue descriptions, comments and other
-- wiki pages (KUA-98), so a page's backlinks are an index lookup instead of a
-- scan of all content. Triggers keep it in step with the content on every
-- write path. They read the one form a mention is stored in
-- (sanitize.WikiMentionHTML); the strict UUID pattern makes the cast safe for
-- content that predates the sanitizer. Rows carry no title, and page_id has no
-- foreign key because content may mention a page that no longer exists.
CREATE TABLE wiki_page_mentions (
    page_id UUID NOT NULL,
    issue_id UUID REFERENCES issues(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comments(id) ON DELETE CASCADE,
    source_page_id UUID REFERENCES wiki_pages(id) ON DELETE CASCADE,
    CHECK (num_nonnulls(issue_id, comment_id, source_page_id) = 1)
);
CREATE UNIQUE INDEX wiki_page_mentions_issue_idx ON wiki_page_mentions (issue_id, page_id) WHERE issue_id IS NOT NULL;
CREATE UNIQUE INDEX wiki_page_mentions_comment_idx ON wiki_page_mentions (comment_id, page_id) WHERE comment_id IS NOT NULL;
CREATE UNIQUE INDEX wiki_page_mentions_source_page_idx ON wiki_page_mentions (source_page_id, page_id) WHERE source_page_id IS NOT NULL;
CREATE INDEX wiki_page_mentions_page_idx ON wiki_page_mentions (page_id);

CREATE FUNCTION kuayle_wiki_mention_ids(content TEXT) RETURNS SETOF UUID LANGUAGE sql IMMUTABLE AS $$
    SELECT DISTINCT found[1]::uuid FROM regexp_matches(COALESCE(content, ''),
        '<span data-type="mention" data-kind="wiki" data-id="([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"', 'g') AS found;
$$;

CREATE FUNCTION kuayle_index_issue_wiki_mentions() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        DELETE FROM wiki_page_mentions WHERE issue_id = NEW.id;
    END IF;
    INSERT INTO wiki_page_mentions (page_id, issue_id) SELECT mentioned.page_id, NEW.id FROM kuayle_wiki_mention_ids(NEW.description) AS mentioned(page_id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION kuayle_index_comment_wiki_mentions() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        DELETE FROM wiki_page_mentions WHERE comment_id = NEW.id;
    END IF;
    INSERT INTO wiki_page_mentions (page_id, comment_id) SELECT mentioned.page_id, NEW.id FROM kuayle_wiki_mention_ids(NEW.body) AS mentioned(page_id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION kuayle_index_page_wiki_mentions() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        DELETE FROM wiki_page_mentions WHERE source_page_id = NEW.id;
    END IF;
    INSERT INTO wiki_page_mentions (page_id, source_page_id) SELECT mentioned.page_id, NEW.id FROM kuayle_wiki_mention_ids(NEW.content) AS mentioned(page_id);
    RETURN NULL;
END;
$$;

-- Content without a mention costs one substring test on insert, and updates
-- that leave the content alone cost nothing.
CREATE TRIGGER issues_wiki_mentions_insert AFTER INSERT ON issues FOR EACH ROW
    WHEN (NEW.description LIKE '%data-kind="wiki"%') EXECUTE FUNCTION kuayle_index_issue_wiki_mentions();
CREATE TRIGGER issues_wiki_mentions_update AFTER UPDATE OF description ON issues FOR EACH ROW
    WHEN (OLD.description IS DISTINCT FROM NEW.description) EXECUTE FUNCTION kuayle_index_issue_wiki_mentions();
CREATE TRIGGER comments_wiki_mentions_insert AFTER INSERT ON comments FOR EACH ROW
    WHEN (NEW.body LIKE '%data-kind="wiki"%') EXECUTE FUNCTION kuayle_index_comment_wiki_mentions();
CREATE TRIGGER comments_wiki_mentions_update AFTER UPDATE OF body ON comments FOR EACH ROW
    WHEN (OLD.body IS DISTINCT FROM NEW.body) EXECUTE FUNCTION kuayle_index_comment_wiki_mentions();
CREATE TRIGGER wiki_pages_wiki_mentions_insert AFTER INSERT ON wiki_pages FOR EACH ROW
    WHEN (NEW.content LIKE '%data-kind="wiki"%') EXECUTE FUNCTION kuayle_index_page_wiki_mentions();
CREATE TRIGGER wiki_pages_wiki_mentions_update AFTER UPDATE OF content ON wiki_pages FOR EACH ROW
    WHEN (OLD.content IS DISTINCT FROM NEW.content) EXECUTE FUNCTION kuayle_index_page_wiki_mentions();

-- Mentions written before this index existed.
INSERT INTO wiki_page_mentions (page_id, issue_id)
    SELECT mentioned.page_id, issues.id FROM issues, kuayle_wiki_mention_ids(issues.description) AS mentioned(page_id) WHERE issues.description LIKE '%data-kind="wiki"%';
INSERT INTO wiki_page_mentions (page_id, comment_id)
    SELECT mentioned.page_id, comments.id FROM comments, kuayle_wiki_mention_ids(comments.body) AS mentioned(page_id) WHERE comments.body LIKE '%data-kind="wiki"%';
INSERT INTO wiki_page_mentions (page_id, source_page_id)
    SELECT mentioned.page_id, wiki_pages.id FROM wiki_pages, kuayle_wiki_mention_ids(wiki_pages.content) AS mentioned(page_id) WHERE wiki_pages.content LIKE '%data-kind="wiki"%';
