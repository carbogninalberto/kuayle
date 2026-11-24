package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"time"

	"github.com/google/uuid"
)

// CompleteMergedPullRequest finalizes tasks waiting on the merged pull request.
// GitHub is the authority for merges; a redelivered or
// unrelated webhook changes nothing. It returns the number of completed tasks.
func (r *AgentTaskRepository) CompleteMergedPullRequest(ctx context.Context, workspaceID uuid.UUID, repository string, number int) (int, error) {
	var candidates []uuid.UUID
	if err := r.db.SelectContext(ctx, &candidates, `SELECT DISTINCT t.id FROM agent_tasks t JOIN dev_machine_git_refs g ON g.agent_run_id=`+reviewPublicationRun+` AND g.workspace_id=t.workspace_id
 WHERE t.workspace_id=$1 AND t.state='awaiting_review' AND t.workflow_status IS DISTINCT FROM 'entering'
 AND g.ref_type='pull_request' AND LOWER(g.repository_full_name)=LOWER($2) AND g.pull_request_number=$3`, workspaceID, repository, number); err != nil {
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
	// Re-check under the task lock in case a follow-up or switch raced the query.
	var current bool
	if err = tx.GetContext(ctx, &current, `SELECT EXISTS(SELECT 1 FROM agent_tasks t
 JOIN dev_machine_git_refs g ON g.agent_run_id=`+reviewPublicationRun+` AND g.workspace_id=t.workspace_id
 WHERE t.id=$1 AND t.workspace_id=$2 AND t.state='awaiting_review' AND t.workflow_status IS DISTINCT FROM 'entering'
 AND g.ref_type='pull_request' AND LOWER(g.repository_full_name)=LOWER($3) AND g.pull_request_number=$4)`, task.ID, workspaceID, repository, number); err != nil {
		return false, err
	}
	if !current {
		return false, nil
	}
	// A merged transition into a supervised end still has work to do. Let
	// the runner enter that end under its usual lease, including on redelivery.
	// A pipeline's tick needs nothing more: the merge completes its task.
	if task.RunsWorkflow() && !task.Pipeline {
		var raw json.RawMessage
		if err = tx.GetContext(ctx, &raw, `SELECT graph FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, task.WorkflowVersionID, workspaceID); err != nil {
			return false, err
		}
		graph, err := domain.ParseWorkflowGraph(raw)
		if err != nil {
			return false, err
		}
		var step domain.WorkflowStep
		if err = tx.GetContext(ctx, &step, `SELECT * FROM agent_task_workflow_steps WHERE task_id=$1 AND sequence=$2`, task.ID, task.WorkflowSequence); err != nil {
			return false, err
		}
		outcome := domain.WorkflowMerged
		if wait := step.MergeWait(); step.NodeType == domain.WorkflowNodeApproval && wait != nil && wait.ActorID != nil && wait.Failure == "" {
			outcome = domain.WorkflowApprovalApproved
		}
		to := graph.Decide(task.Cursor(), outcome, time.Now(), nil).To
		if (step.NodeType == domain.WorkflowNodeMerge || outcome == domain.WorkflowApprovalApproved) && to.Type == domain.WorkflowNodeEnd && to.Supervised() && step.FinishedAt == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE agent_task_workflow_steps SET stage_merge_observed=true WHERE task_id=$1 AND sequence=$2`, task.ID, step.Sequence); err != nil {
				return false, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, task.ID); err != nil {
				return false, err
			}
			return false, tx.Commit()
		}
	}
	if err = transitionAgentTask(ctx, tx, task, "completed", fmt.Sprintf("Pull request #%d was merged on GitHub", number), nil); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ReviewPullRequest is a pull request recorded for a task attempt in review.
type ReviewPullRequest struct {
	WorkspaceID uuid.UUID `db:"workspace_id"`
	Repository  string    `db:"repository_full_name"`
	Number      int       `db:"pull_request_number"`
}

// ReviewPullRequests lists the pull requests of tasks awaiting review, least
// recently updated first, so a reconciler can catch merges whose webhook was
// lost (GitHub does not redeliver failed deliveries).
func (r *AgentTaskRepository) ReviewPullRequests(ctx context.Context, limit int) ([]ReviewPullRequest, error) {
	pulls := []ReviewPullRequest{}
	err := r.db.SelectContext(ctx, &pulls, `SELECT DISTINCT ON (t.workspace_id, LOWER(g.repository_full_name), g.pull_request_number) t.workspace_id, g.repository_full_name, g.pull_request_number
 FROM agent_tasks t JOIN dev_machine_git_refs g ON g.agent_run_id=`+reviewPublicationRun+` AND g.workspace_id=t.workspace_id
 WHERE t.state='awaiting_review' AND t.workflow_status IS DISTINCT FROM 'entering' AND g.ref_type='pull_request' AND g.pull_request_number IS NOT NULL
 ORDER BY t.workspace_id, LOWER(g.repository_full_name), g.pull_request_number, t.updated_at LIMIT $1`, limit)
	return pulls, err
}
