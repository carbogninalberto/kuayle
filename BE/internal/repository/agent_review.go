package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CompleteMergedPullRequest finalizes tasks whose current attempt recorded the
// merged pull request. GitHub is the authority for merges; a redelivered or
// unrelated webhook changes nothing. It returns the number of completed tasks.
func (r *AgentTaskRepository) CompleteMergedPullRequest(ctx context.Context, workspaceID uuid.UUID, repository string, number int) (int, error) {
	var candidates []uuid.UUID
	if err := r.db.SelectContext(ctx, &candidates, `SELECT DISTINCT t.id FROM agent_tasks t JOIN dev_machine_git_refs g ON g.agent_run_id=t.run_id AND g.workspace_id=t.workspace_id
 WHERE t.workspace_id=$1 AND t.state='awaiting_review' AND g.ref_type='pull_request' AND LOWER(g.repository_full_name)=LOWER($2) AND g.pull_request_number=$3`, workspaceID, repository, number); err != nil {
		return 0, err
	}
	completed := 0
	for _, id := range candidates {
		done, err := r.completeMerged(ctx, id, workspaceID, repository, number)
		if err != nil {
			return completed, err
		}
		if done {
			completed++
		}
	}
	return completed, nil
}

func (r *AgentTaskRepository) completeMerged(ctx context.Context, id, workspaceID uuid.UUID, repository string, number int) (bool, error) {
	tx, task, err := r.transaction(ctx, id, false)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Re-check under the task lock: a concurrent follow-up may have replaced the
	// attempt, and only the current attempt's recorded pull request counts.
	var current bool
	if err = tx.GetContext(ctx, &current, `SELECT EXISTS(SELECT 1 FROM dev_machine_git_refs WHERE agent_run_id=$1 AND workspace_id=$2 AND ref_type='pull_request' AND LOWER(repository_full_name)=LOWER($3) AND pull_request_number=$4)`, task.RunID, workspaceID, repository, number); err != nil {
		return false, err
	}
	if task.WorkspaceID != workspaceID || task.State != "awaiting_review" || !current {
		return false, nil
	}
	if err = transitionAgentTask(ctx, tx, task, "completed", fmt.Sprintf("Pull request #%d was merged on GitHub", number), nil); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
