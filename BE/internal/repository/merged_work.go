package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// MergedPullRequest returns the newest merged pull request (GitHub) or merge
// request (other forges) linked to the issue, or nil. Only rows of the
// workspace count, and only for a caller who can see the issue.
func (r *AgentTaskRepository) MergedPullRequest(ctx context.Context, ws, issue uuid.UUID) (*domain.MergedPullRequest, error) {
	return mergedPullRequest(ctx, r.db, ws, issue)
}

func mergedPullRequest(ctx context.Context, q sqlx.QueryerContext, ws, issue uuid.UUID) (*domain.MergedPullRequest, error) {
	var merged domain.MergedPullRequest
	err := sqlx.GetContext(ctx, q, &merged, `SELECT number,url,repository FROM (
 SELECT p.number,p.html_url AS url,r.full_name AS repository,COALESCE(p.merged_at,p.updated_at) AS merged_at
 FROM github_pull_requests p JOIN github_repos r ON r.id=p.github_repo_id AND r.workspace_id=p.workspace_id
 WHERE p.workspace_id=$1 AND p.issue_id=$2 AND (p.state='merged' OR p.merged_at IS NOT NULL)
 UNION ALL
 SELECT m.number,m.web_url,r.full_name,COALESCE(m.merged_at,m.updated_at)
 FROM forge_merge_requests m JOIN forge_repos r ON r.id=m.forge_repo_id AND r.workspace_id=m.workspace_id
 WHERE m.workspace_id=$1 AND m.issue_id=$2 AND (m.state='merged' OR m.merged_at IS NOT NULL)
) merged WHERE `+workspaceVisible(ctx, "CAST($1 AS uuid)")+` AND `+issueVisible(ctx, "CAST($2 AS uuid)")+`
 ORDER BY merged_at DESC LIMIT 1`, ws, issue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &merged, nil
}

// refuseMergedWork is a MergedWorkError when the task's issue has merged work
// and the request did not confirm starting another run past it. Callers hold
// the task's transaction and have already authorized the task.
func refuseMergedWork(ctx context.Context, tx *sqlx.Tx, task *domain.AgentTask, confirmed bool) error {
	if confirmed {
		return nil
	}
	merged, err := mergedPullRequest(ctx, tx, task.WorkspaceID, task.IssueID)
	if err != nil || merged == nil {
		return err
	}
	return &domain.MergedWorkError{PullRequest: *merged}
}
