package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// EnsurePublication queues the single publication of an independently validated
// attempt. The task lease, step and operation commit together; replays return
// the existing step instead of publishing twice.
func (r *AgentTaskRepository) EnsurePublication(ctx context.Context, claimed domain.AgentTask) (*domain.DevMachineAgentRunStep, error) {
	tx, task, err := r.transaction(ctx, claimed.ID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return nil, err
	}
	if task.State != publicationPhase.state || task.MachineID == nil {
		return nil, ErrAgentTaskConflict
	}
	step := &domain.DevMachineAgentRunStep{}
	err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=1 AND step_type='publication'`, task.RunID)
	if err == nil {
		if err = failExhaustedStep(ctx, tx, *task, step, publicationPhase, "Publishing could not finish"); err != nil {
			return nil, err
		}
		return step, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	generation, err := lockTaskMachine(ctx, tx, *task)
	if err != nil {
		return nil, err
	}
	if err = tx.GetContext(ctx, step, `INSERT INTO dev_machine_agent_run_steps(agent_run_id,sequence,step_type,name,status,command_argv,summary) VALUES($1,1,'publication','Publish changes','queued','[]','Waiting to publish the validated changes') RETURNING *`, task.RunID); err != nil {
		return nil, err
	}
	op := &domain.DevMachineOperation{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, AgentRunID: &task.RunID, Action: domain.DevMachineOpPublishAgent, Status: domain.DevMachineOpStatusPending, Generation: generation, IdempotencyKey: "task-publication:" + task.RunID.String(), RequestedByUserID: &task.CreatorID, MaxAttempts: 3}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return nil, err
	}
	return step, tx.Commit()
}

// GetPublication re-verifies publication authority for the leased operation and
// returns the issue reference used in the commit and pull request.
func (r *DevMachineRepository) GetPublication(ctx context.Context, op domain.DevMachineOperation) (*domain.AgentPublication, error) {
	tx, task, step, err := r.taskStepTransaction(ctx, op, publicationPhase)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	publication := &domain.AgentPublication{Step: *step}
	err = tx.QueryRowContext(ctx, `SELECT identifier_text,title FROM issues WHERE id=$1 AND workspace_id=$2`, task.IssueID, task.WorkspaceID).Scan(&publication.IssueIdentifier, &publication.IssueTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentTaskUnauthorized
	}
	if err != nil {
		return nil, err
	}
	var name, email sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT agent_git_name,agent_git_email FROM dev_machine_workspace_policies WHERE workspace_id=$1`, task.WorkspaceID).Scan(&name, &email); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	publication.AuthorName, publication.AuthorEmail = name.String, email.String
	// The branch's last published commit, from Kuayle's own record.
	var published sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT g.commit_sha FROM dev_machine_agent_runs r
 JOIN dev_machine_checkouts c ON c.id=r.checkout_id AND c.workspace_id=r.workspace_id
 JOIN dev_machine_git_refs g ON g.workspace_id=r.workspace_id AND g.repository_full_name=c.repository_full_name AND g.ref_name=c.working_branch AND g.ref_type='commit'
 WHERE r.id=$1 AND g.commit_sha IS NOT NULL ORDER BY g.created_at DESC LIMIT 1`, task.RunID).Scan(&published); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	publication.PublishedCommit = published.String
	var argv []byte
	err = tx.QueryRowContext(ctx, `SELECT command_argv,exit_code FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=0 AND step_type='validation' AND status='succeeded'`, task.RunID).Scan(&argv, &publication.CheckExitCode)
	if err == nil {
		_ = json.Unmarshal(argv, &publication.CheckArgv)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return publication, nil
}

func (r *DevMachineRepository) StartPublication(ctx context.Context, op domain.DevMachineOperation) error {
	tx, task, step, err := r.taskStepTransaction(ctx, op, publicationPhase)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusQueued && step.Status != domain.DevMachineAgentRunStepStatusRunning {
		return ErrAgentTaskConflict
	}
	if step.Status == domain.DevMachineAgentRunStepStatusRunning {
		return nil
	}
	if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='running',started_at=COALESCE(started_at,now()),summary='Publishing the validated changes' WHERE id=$1 RETURNING *`, step.ID); err != nil {
		return err
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return err
	}
	return tx.Commit()
}

// CompletePublication records GitHub's results with the step outcome. Branch,
// commit and pull request references commit in the same transaction.
func (r *DevMachineRepository) CompletePublication(ctx context.Context, op domain.DevMachineOperation, result domain.AgentPublicationResult) error {
	tx, task, step, err := r.taskStepTransaction(ctx, op, publicationPhase)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if step.Status != domain.DevMachineAgentRunStepStatusRunning || step.StartedAt == nil {
		return ErrAgentTaskConflict
	}
	if result.Commit == "" {
		if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='skipped',completed_at=now(),summary='The attempt produced no changes to publish' WHERE id=$1 RETURNING *`, step.ID); err != nil {
			return err
		}
	} else {
		if result.Branch == "" || result.PullRequestNumber <= 0 || result.PullRequestURL == "" {
			return ErrAgentTaskConflict
		}
		if err = tx.GetContext(ctx, step, `UPDATE dev_machine_agent_run_steps SET status='succeeded',completed_at=now(),summary=$2 WHERE id=$1 RETURNING *`, step.ID, result.PullRequestURL); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET branch=$2,pull_request_url=$3 WHERE id=$1`, step.AgentRunID, result.Branch, result.PullRequestURL); err != nil {
			return err
		}
		// Git refs reuse the checkout's repository so a manipulated result cannot
		// attribute the pull request to another repository.
		refs, err := tx.ExecContext(ctx, `INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,issue_id,ref_type,repository_full_name,ref_name,commit_sha,pull_request_number,url)
 SELECT r.workspace_id,r.machine_id,r.id,c.issue_id,ref.kind,c.repository_full_name,$2,ref.sha,ref.number,ref.url
 FROM dev_machine_agent_runs r JOIN dev_machine_checkouts c ON c.id=r.checkout_id AND c.workspace_id=r.workspace_id
 CROSS JOIN (VALUES ('branch',NULL::varchar,NULL::int,NULL::text),('commit',$3::varchar,NULL::int,NULL::text),('pull_request',NULL::varchar,$4::int,$5::text)) AS ref(kind,sha,number,url)
 WHERE r.id=$1`, step.AgentRunID, result.Branch, result.Commit, result.PullRequestNumber, result.PullRequestURL)
		if err != nil {
			return err
		}
		if n, _ := refs.RowsAffected(); n != 3 {
			return ErrAgentTaskConflict
		}
	}
	if err = validationEvent(ctx, tx, *task, step); err != nil {
		return err
	}
	return tx.Commit()
}

// AgentPublicationView is the read model for one attempt's publication.
type AgentPublicationView struct {
	Step           *domain.DevMachineAgentRunStep `json:"step"`
	Branch         *string                        `json:"branch,omitempty"`
	PullRequestURL *string                        `json:"pull_request_url,omitempty"`
}

func (r *AgentTaskRepository) Publication(ctx context.Context, ws, id, run uuid.UUID) (*AgentPublicationView, error) {
	if _, err := conversationActor(ctx, ws); err != nil {
		return nil, err
	}
	tx, _, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	view := &AgentPublicationView{}
	err = tx.QueryRowContext(ctx, `SELECT branch,pull_request_url FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3`, run, id, ws).Scan(&view.Branch, &view.PullRequestURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	step := &domain.DevMachineAgentRunStep{}
	err = tx.GetContext(ctx, step, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 AND sequence=1 AND step_type='publication'`, run)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		view.Step = step
	}
	// Only a recorded publication exposes review links; an agent-reported URL from
	// native output never appears as the task's pull request.
	if view.Step == nil || view.Step.Status != domain.DevMachineAgentRunStepStatusSucceeded {
		view.PullRequestURL = nil
	}
	return view, nil
}
