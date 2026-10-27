package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Start atomically records observed native execution and conditionally moves its
// unchanged issue to the team's configured started status. It never completes an
// issue. The caller publishes the returned issue only after this commit succeeds.
func (r *AgentTaskRepository) Start(ctx context.Context, claimed domain.AgentTask) (*domain.Issue, error) {
	tx, err := beginConversationTransaction(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Acquire the issue write lock BEFORE the conversation/task locks. Upgrading a
	// shared issue lock later deadlocks when different users have chats on one issue.
	var issueID uuid.UUID
	err = tx.GetContext(ctx, &issueID, `SELECT i.id FROM issues i JOIN agent_tasks t ON t.issue_id=i.id AND t.workspace_id=i.workspace_id WHERE t.id=$1 FOR UPDATE OF i`, claimed.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	task, err := lockAgentTask(ctx, tx, claimed.ID, true)
	if err != nil {
		return nil, err
	}
	if err = fenceAgentTask(ctx, tx, *task, claimed); err != nil {
		return nil, err
	}
	if task.State != "preparing" {
		return nil, ErrAgentTaskConflict
	}
	var started bool
	err = tx.GetContext(ctx, &started, `SELECT started_at IS NOT NULL AND status IN ('running','waiting_input','succeeded','failed','timeout') FROM dev_machine_agent_runs WHERE id=$1 AND task_id=$2 AND workspace_id=$3 AND machine_id=$4 AND requested_by_user_id=$5 FOR SHARE`, task.RunID, task.ID, task.WorkspaceID, task.MachineID, task.CreatorID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !started) {
		return nil, ErrAgentTaskConflict
	}
	if err != nil {
		return nil, err
	}
	if err = transitionAgentTask(ctx, tx, task, "running", "Native coding harness started", nil); err != nil {
		return nil, err
	}
	var target struct {
		ID      uuid.UUID `db:"id"`
		Slug    string    `db:"slug"`
		Name    string    `db:"name"`
		OldName string    `db:"old_name"`
	}
	err = tx.GetContext(ctx, &target, `SELECT target.id,target.slug,target.name,previous.name AS old_name FROM issues i
 JOIN team_statuses previous ON previous.id=i.status_id AND previous.team_id=i.team_id
 JOIN team_statuses target ON target.team_id=i.team_id AND target.category='started'
 WHERE i.id=$1 AND i.workspace_id=$2 AND i.team_id=$3 AND i.status_id=$4 AND i.updated_at=$5
 AND previous.category IN ('backlog','unstarted')
 ORDER BY (target.slug='in_progress') DESC,target.position,target.id LIMIT 1`, task.IssueID, task.WorkspaceID, task.TeamID, task.IssueStatusID, task.IssueUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	var issue domain.Issue
	if err = tx.GetContext(ctx, &issue, `UPDATE issues SET status_id=$2,status=$3,updated_at=now() WHERE id=$1 RETURNING *`, task.IssueID, target.ID, target.Slug); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO issue_history(id,issue_id,user_id,field,old_value,new_value,agent) VALUES($1,$2,$3,'status',$4,$5,$6)`, uuid.New(), task.IssueID, task.CreatorID, target.OldName, target.Name, task.Provider); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &issue, nil
}
