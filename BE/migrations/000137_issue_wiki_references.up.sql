-- Wiki pages attached to an issue as references (KUA-98). References written
-- inside issue descriptions, comments and other pages live in that content as
-- mention chips holding only the page ID; this table holds the ones attached
-- to the issue itself. Rows carry no title: every read joins the page and
-- applies the reader's visibility of both the issue and the page.
CREATE TABLE issue_wiki_references (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    page_id UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (issue_id, page_id)
);
CREATE INDEX issue_wiki_references_page_idx ON issue_wiki_references (page_id);
