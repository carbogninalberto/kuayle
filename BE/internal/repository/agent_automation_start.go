package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

const maxAutomationAttempts = 3
const automationTaskReceipt = `SELECT id FROM agent_tasks WHERE workspace_id=$1 AND creator_id=$2 AND issue_id=$3 AND request_id=$4`

// LeaseStart serializes starts from approvals and workers. Recover the task
// receipt first, including a crash on the last allowed attempt.
func (r *AgentAutomationRepository) LeaseStart(ctx context.Context, ws, id uuid.UUID) (*domain.AgentAutomationRunView, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var run domain.AgentAutomationRunView
	err = tx.GetContext(ctx, &run, `SELECT `+runViewColumns+` FROM agent_automation_runs r
 JOIN agent_automations a ON a.id=r.automation_id JOIN issues i ON i.id=r.issue_id
 WHERE r.id=$1 AND r.workspace_id=$2 AND r.status='starting' AND r.available_at<=now()
 FOR UPDATE OF r SKIP LOCKED`, id, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var task uuid.UUID
	err = tx.GetContext(ctx, &task, automationTaskReceipt, ws, run.ActorID, run.IssueID, run.RequestID)
	switch {
	case err == nil:
		_, err = tx.ExecContext(ctx, `UPDATE agent_automation_runs SET status='started',task_id=$2,error=NULL,lease_id=NULL,updated_at=now() WHERE id=$1`, id, task)
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	case run.Attempts >= maxAutomationAttempts:
		_, err = tx.ExecContext(ctx, `UPDATE agent_automation_runs SET status='failed',error='Worker interrupted before task creation',lease_id=NULL,updated_at=now() WHERE id=$1`, id)
	default:
		var needsApproval bool
		err = tx.GetContext(ctx, &needsApproval, `SELECT a.unattended AND NOT $2 AND NOT EXISTS(SELECT 1 FROM dev_machine_workspace_policies p WHERE p.workspace_id=a.workspace_id AND p.enabled AND p.allow_unattended_automations) FROM agent_automations a WHERE a.id=$1`, run.AutomationID, run.Approved)
		if err != nil {
			return nil, err
		}
		if needsApproval {
			_, err = tx.ExecContext(ctx, `UPDATE agent_automation_runs SET status='pending_approval',actor_id=NULL,attempts=0,error=NULL,lease_id=NULL,updated_at=now() WHERE id=$1`, id)
			if err == nil {
				err = recordAutomationApproval(ctx, tx, run)
			}
		} else {
			err = tx.QueryRowxContext(ctx, `UPDATE agent_automation_runs SET lease_id=gen_random_uuid(),attempts=attempts+1,available_at=now()+interval '2 minutes',updated_at=now()
 WHERE id=$1 RETURNING lease_id,attempts`, id).Scan(&run.LeaseID, &run.Attempts)
			if err != nil {
				return nil, err
			}
			return &run, tx.Commit()
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// Finish fences stale completions with the lease. Only a transient start
// failure is retried, with 30s then 60s backoff and a durable operator reason.
func (r *AgentAutomationRepository) Finish(ctx context.Context, run domain.AgentAutomationRunView, task *uuid.UUID, reason string, retry bool) error {
	// A connection failure can mask a successful commit, even on the final
	// attempt. Settle the existing receipt before reporting a failed start.
	if retry {
		var accepted uuid.UUID
		err := r.db.GetContext(ctx, &accepted, automationTaskReceipt, run.WorkspaceID, run.ActorID, run.IssueID, run.RequestID)
		if err == nil {
			task, reason = &accepted, ""
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	status := "started"
	if task == nil {
		status = "failed"
		if retry && run.Attempts < maxAutomationAttempts {
			status = "starting"
		}
	}
	_, err := r.db.ExecContext(ctx, `UPDATE agent_automation_runs SET status=$3,task_id=$4,error=NULLIF($5,''),lease_id=NULL,
 available_at=now()+make_interval(secs=>30*attempts),updated_at=now()
 WHERE id=$1 AND lease_id=$2 AND status='starting'`, run.ID, run.LeaseID, status, task, domain.TruncateBytes(reason, 1024))
	return err
}

// Replay keeps the original request and actor. Lock the same rule as Claim so
// a replay cannot take a capacity slot concurrently allocated to a new match.
func (r *AgentAutomationRepository) Replay(ctx context.Context, ws, id, actor uuid.UUID, admin bool) (*domain.AgentAutomationRunView, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var rule domain.AgentAutomation
	err = tx.GetContext(ctx, &rule, `SELECT a.* FROM agent_automations a JOIN agent_automation_runs r ON a.id=r.automation_id
 WHERE r.id=$1 AND r.workspace_id=$2 AND r.status='failed' AND r.actor_id=$3 AND a.enabled AND ($4 OR a.owner_id=$3)
 AND `+teamVisible(ctx, "a.team_id")+` AND `+issueVisible(ctx, "r.issue_id")+` FOR UPDATE OF a,r`, id, ws, actor, admin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAutomationNotFound
	}
	if err != nil {
		return nil, err
	}
	var active int
	if err = tx.GetContext(ctx, &active, automationActiveCount, rule.ID); err != nil {
		return nil, err
	}
	if active >= rule.MaxActive {
		return nil, ErrAgentTaskConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE agent_automation_runs SET status='starting',attempts=0,replays=replays+1,approved=true,
 lease_id=NULL,error=NULL,available_at=now(),updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.Run(ctx, ws, id)
}

// Store the notice and publish its recipient-only invalidation in the match
// transaction. A rollback delivers neither. The shared visibility predicate
// checks the recipient's current issue, workspace and original team access.
func recordAutomationApproval(ctx context.Context, tx *sqlx.Tx, run domain.AgentAutomationRunView) error {
	recipient := domain.WithAccess(ctx, domain.Access{WorkspaceID: run.WorkspaceID, UserID: run.OwnerID})
	title := fmt.Sprintf("%s wants to start a coding task on %s", run.AutomationName, run.IssueIdentifier)
	_, err := tx.ExecContext(ctx, `WITH inserted AS (
 INSERT INTO notifications(user_id,workspace_id,issue_id,type,title,source_team_ids)
 SELECT n.user_id,n.workspace_id,n.issue_id,'agent_automation_approval',$5,n.source_team_ids
 FROM (SELECT $1::uuid AS user_id,$2::uuid AS workspace_id,$3::uuid AS issue_id,ARRAY[$4::uuid] AS source_team_ids,'agent_automation_approval' AS type) n
 WHERE `+notificationVisible(recipient, "n")+` RETURNING user_id,workspace_id)
 SELECT pg_notify('agent_automation_approval',workspace_id::text||'/'||user_id::text) FROM inserted`,
		run.OwnerID, run.WorkspaceID, run.IssueID, run.TeamID, title)
	return err
}
