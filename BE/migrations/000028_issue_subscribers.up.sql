CREATE TABLE issue_subscribers (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (issue_id, user_id)
);

CREATE INDEX idx_issue_subscribers_user ON issue_subscribers(user_id);
