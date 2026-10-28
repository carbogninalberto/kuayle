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
// the current one: native outcome, independent check, publication and changed
// files. The agent summary is the previous harness's own, untrusted text.
func (r *AgentTaskRepository) PreviousAttempt(ctx context.Context, task domain.AgentTask) (*domain.AgentHandoff, error) {
	var row struct {
		ID           uuid.UUID       `db:"id"`
		MachineID    uuid.UUID       `db:"machine_id"`
		Session      *string         `db:"native_session_id"`
		Status       string          `db:"status"`
		Summary      *string         `db:"summary"`
		ChangedFiles json.RawMessage `db:"changed_files"`
		Validation   *string         `db:"validation"`
		ExitCode     *int            `db:"exit_code"`
		PullRequest  *string         `db:"pull_request"`
	}
	err := r.db.GetContext(ctx, &row, `SELECT r.id,r.machine_id,r.native_session_id,r.status::text AS status,r.summary,r.changed_files,
 v.status::text AS validation,v.exit_code,
 CASE WHEN p.status='succeeded' THEN r.pull_request_url END AS pull_request
 FROM dev_machine_agent_runs r
 LEFT JOIN dev_machine_agent_run_steps v ON v.agent_run_id=r.id AND v.sequence=0 AND v.step_type='validation'
 LEFT JOIN dev_machine_agent_run_steps p ON p.agent_run_id=r.id AND p.sequence=1 AND p.step_type='publication'
 WHERE r.task_id=$1 AND r.workspace_id=$2 AND r.id<>$3 ORDER BY r.created_at DESC,r.id LIMIT 1`, task.ID, task.WorkspaceID, task.RunID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	handoff := &domain.AgentHandoff{AttemptID: row.ID, MachineID: row.MachineID, Status: row.Status, ValidationExitCode: row.ExitCode}
	if row.Session != nil {
		handoff.NativeSessionID = *row.Session
	}
	if row.Summary != nil {
		handoff.Summary = *row.Summary
	}
	if row.Validation != nil {
		handoff.Validation = *row.Validation
	}
	if row.PullRequest != nil {
		handoff.PullRequestURL = *row.PullRequest
	}
	_ = json.Unmarshal(row.ChangedFiles, &handoff.ChangedFiles)
	return handoff, nil
}
