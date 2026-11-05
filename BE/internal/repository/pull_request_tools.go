package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// PullRequestTarget is the pull request Kuayle published for an issue's newest
// task that has one, in any task state: reading a pull request does not need the
// task to await review.
type PullRequestTarget struct {
	TaskID     uuid.UUID `db:"id"`
	IssueID    uuid.UUID `db:"issue_id"`
	CreatorID  uuid.UUID `db:"creator_id"`
	State      string    `db:"state"`
	Repository string    `db:"repository_full_name"`
	Number     int       `db:"pull_request_number"`
	URL        string    `db:"url"`
	// Commit is the commit Kuayle last published; empty if none is recorded.
	Commit string `db:"commit_sha"`
}

// ErrNoPullRequest means no task of the issue published a pull request.
var ErrNoPullRequest = errors.New("no pull request published for the issue")

// PullRequestTarget finds the newest task of an issue that published a pull
// request. Callers authorize the issue first.
func (r *AgentTaskRepository) PullRequestTarget(ctx context.Context, ws, issue uuid.UUID) (*PullRequestTarget, error) {
	var target PullRequestTarget
	err := r.db.GetContext(ctx, &target, `SELECT t.id,t.issue_id,t.creator_id,t.state,p.repository_full_name,p.pull_request_number,p.url,COALESCE(c.commit_sha,'') AS commit_sha
 FROM agent_tasks t
 JOIN LATERAL (SELECT g.repository_full_name,g.pull_request_number,g.url FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='pull_request' AND g.source='published' AND g.pull_request_number IS NOT NULL AND g.url IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) p ON true
 LEFT JOIN LATERAL (SELECT g.commit_sha FROM dev_machine_git_refs g WHERE g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id AND g.ref_type='commit' AND g.source='published' AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC LIMIT 1) c ON true
 WHERE t.workspace_id=$1 AND t.kind='implement' AND t.issue_id=$2
 ORDER BY t.updated_at DESC LIMIT 1`, ws, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoPullRequest
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// GetMergeMethod returns the configured method of a repository, or "".
func (r *AgentTaskRepository) GetMergeMethod(ctx context.Context, ws uuid.UUID, repository string) (string, error) {
	var method string
	err := r.db.GetContext(ctx, &method, `SELECT merge_method FROM repository_merge_methods WHERE workspace_id=$1 AND repository_full_name=$2`, ws, repository)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return method, err
}

// SetMergeMethod records the method of a repository; an empty method clears it.
func (r *AgentTaskRepository) SetMergeMethod(ctx context.Context, ws uuid.UUID, repository, method string) error {
	if method == "" {
		_, err := r.db.ExecContext(ctx, `DELETE FROM repository_merge_methods WHERE workspace_id=$1 AND repository_full_name=$2`, ws, repository)
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO repository_merge_methods(workspace_id,repository_full_name,merge_method) VALUES($1,$2,$3)
 ON CONFLICT (workspace_id,repository_full_name) DO UPDATE SET merge_method=EXCLUDED.merge_method,updated_at=now()`, ws, repository, method)
	return err
}

// ListMergeMethods returns the configured method of every repository, by full name.
func (r *AgentTaskRepository) ListMergeMethods(ctx context.Context, ws uuid.UUID) (map[string]string, error) {
	var rows []struct {
		Repository string `db:"repository_full_name"`
		Method     string `db:"merge_method"`
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT repository_full_name,merge_method FROM repository_merge_methods WHERE workspace_id=$1`, ws); err != nil {
		return nil, err
	}
	methods := make(map[string]string, len(rows))
	for _, row := range rows {
		methods[row.Repository] = row.Method
	}
	return methods, nil
}
