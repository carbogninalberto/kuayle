package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// PreviousAttempt returns what Kuayle recorded about the task's attempt before
// the current one: native outcome, publication and changed
// files. The agent summary is the previous harness's own, untrusted text.
func (r *AgentTaskRepository) PreviousAttempt(ctx context.Context, task domain.AgentTask) (*domain.AgentHandoff, error) {
	var row struct {
		ID            uuid.UUID       `db:"id"`
		MachineID     uuid.UUID       `db:"machine_id"`
		Session       *string         `db:"native_session_id"`
		Status        string          `db:"status"`
		Summary       *string         `db:"summary"`
		ChangedFiles  json.RawMessage `db:"changed_files"`
		PullRequest   *string         `db:"pull_request"`
		Publication   *string         `db:"publication_failure"`
		Continued     bool            `db:"continued"`
		NoFinal       bool            `db:"no_final_message"`
		Notebook      *string         `db:"notebook_problem"`
		FailureRule   *string         `db:"failure_rule"`
		PriorWorkflow bool            `db:"prior_workflow"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT r.id,r.machine_id,r.native_session_id,r.status::text AS status,r.summary,r.changed_files,
 CASE WHEN p.status='succeeded' THEN r.pull_request_url END AS pull_request,
 CASE WHEN p.status='failed' THEN p.summary END AS publication_failure,
 r.session_continued_at IS NOT NULL AS continued,
 COALESCE(r.result->>'no_final_message','')='true' AS no_final_message,r.notebook_problem,r.failure_rule,
 EXISTS(SELECT 1 FROM agent_task_workflow_steps s WHERE s.task_id=r.task_id AND s.sequence=$4 AND s.entered_at>r.created_at) AS prior_workflow
 FROM dev_machine_agent_runs r
 LEFT JOIN dev_machine_agent_run_steps p ON p.agent_run_id=r.id AND p.sequence=1 AND p.step_type='publication'
 WHERE r.task_id=$1 AND r.workspace_id=$2 AND r.id<>$3 ORDER BY r.created_at DESC,r.id LIMIT 1`, task.ID, task.WorkspaceID, task.RunID, task.WorkflowSwitchSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	handoff := &domain.AgentHandoff{AttemptID: row.ID, MachineID: row.MachineID, Status: row.Status, ContinuedInteractively: row.Continued, NoFinalMessage: row.NoFinal}
	handoff.PublicationBlock = domain.PublicationCheckoutBlockReason(row.Publication)
	if row.Notebook != nil && domain.IsNotebookNotUpdated(*row.Notebook) {
		handoff.NotebookProblem = *row.Notebook
	}
	if row.FailureRule != nil {
		handoff.FailureRule = *row.FailureRule
	}
	if row.Session != nil {
		handoff.NativeSessionID = *row.Session
	}
	if row.Summary != nil {
		handoff.Summary = *row.Summary
	}
	if row.PullRequest != nil {
		handoff.PullRequestURL = *row.PullRequest
	}
	_ = json.Unmarshal(row.ChangedFiles, &handoff.ChangedFiles)
	// An earlier workflow supplies handoff context, but its session is not
	// used to enter the chosen step, even when the node IDs happen to match.
	if row.PriorWorkflow {
		handoff.OtherProfile = true
	} else if handoff.OtherProfile, err = r.previousStepProfileDiffers(ctx, task); err != nil {
		return nil, err
	}
	return handoff, nil
}

// previousStepProfileDiffers reports whether the workflow step that ran the
// task's previous iteration used another agent profile than the step the
// task is on. A retry or a recreated attempt stays on its step, so it does
// not differ; a task without a workflow never does.
func (r *AgentTaskRepository) previousStepProfileDiffers(ctx context.Context, task domain.AgentTask) (bool, error) {
	if !task.RunsWorkflow() || task.WorkflowNode == nil {
		return false, nil
	}
	var previous string
	err := r.db.GetContext(ctx, &previous, `SELECT node_id FROM agent_task_workflow_steps WHERE task_id=$1 AND generation IS NOT NULL AND sequence<$2 ORDER BY sequence DESC LIMIT 1`, task.ID, task.WorkflowSequence)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && previous == *task.WorkflowNode) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var raw json.RawMessage
	err = r.db.GetContext(ctx, &raw, `SELECT graph FROM workflow_versions WHERE id=$1 AND workspace_id=$2`, task.WorkflowVersionID, task.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var graph domain.WorkflowGraph
	if err = json.Unmarshal(raw, &graph); err != nil {
		return false, err
	}
	was, _ := graph.Node(previous)
	is, _ := graph.Node(*task.WorkflowNode)
	return was.ProfileID != nil && is.ProfileID != nil && *was.ProfileID != *is.ProfileID, nil
}

// MarkSessionContinued records that the user opened the task's native
// session in Claude Code on machineID, on the attempt that recorded it there.
func (r *AgentTaskRepository) MarkSessionContinued(ctx context.Context, ws, taskID, machineID uuid.UUID, nativeSessionID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET session_continued_at=now()
 WHERE workspace_id=$1 AND task_id=$2 AND machine_id=$3 AND native_session_id=$4`, ws, taskID, machineID, nativeSessionID)
	return err
}
