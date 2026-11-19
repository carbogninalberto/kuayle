package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Match the task's latest published PR on this branch, including earlier
// attempts. An issue may have several tasks and unrelated pull requests.
const taskResourceCleanupCandidates = `
 FROM agent_tasks t
 JOIN dev_machine_checkouts c ON c.id=t.checkout_id AND c.machine_id=t.machine_id
   AND c.workspace_id=t.workspace_id AND c.issue_id=t.issue_id AND c.scope_key=t.id::text
 JOIN dev_machines m ON m.id=c.machine_id AND m.workspace_id=c.workspace_id
 LEFT JOIN LATERAL (
   SELECT g.pull_request_number
   FROM dev_machine_agent_runs r
   JOIN dev_machine_git_refs g ON g.agent_run_id=r.id AND g.workspace_id=r.workspace_id AND g.machine_id=r.machine_id
   WHERE t.state='failed' AND r.task_id=t.id AND r.workspace_id=t.workspace_id
     AND g.ref_type='pull_request' AND g.source='published'
     AND lower(g.repository_full_name)=lower(c.repository_full_name) AND g.ref_name=c.working_branch
   ORDER BY g.created_at DESC, g.id DESC LIMIT 1
 ) ref ON true
 WHERE m.status=m.desired_status AND m.delete_requested_at IS NULL AND m.workspace_volume_name IS NOT NULL
   AND (m.status='running' OR (m.status IN ('paused','stopped') AND NOT EXISTS (
     SELECT 1 FROM dev_machine_services s WHERE s.machine_id=m.id AND s.service_type='docker'
   )))
   AND t.updated_at <= $1::timestamptz - interval '30 minutes'
   AND (
     t.state IN ('completed','cancelled')
     OR (t.state='failed' AND ref.pull_request_number IS NOT NULL AND (
       t.updated_at <= $1::timestamptz - interval '7 days'
       OR EXISTS (SELECT 1 FROM github_pull_requests p
         WHERE p.workspace_id=t.workspace_id AND p.github_repo_id=c.github_repo_id
           AND p.number=ref.pull_request_number AND p.state IN ('closed','merged'))
     ))
   )
   AND t.preview_state IS DISTINCT FROM 'starting' AND t.preview_state IS DISTINCT FROM 'running'
   AND NOT EXISTS (SELECT 1 FROM dev_machine_terminal_sessions s
     WHERE s.checkout_id=c.id AND s.status <> 'closed')
   AND NOT EXISTS (SELECT 1 FROM dev_machine_agent_runs r
     WHERE r.task_id=t.id AND r.workspace_id=t.workspace_id AND r.machine_id=m.id
       AND r.status IN ('queued','starting','running','waiting_input'))
   AND NOT EXISTS (SELECT 1 FROM dev_machine_operations active
     WHERE active.machine_id=m.id AND active.id<>$2 AND active.status IN ('pending','leased')
       AND (active.checkout_id=c.id OR active.agent_task_id=t.id OR active.agent_run_id IN (
         SELECT r.id FROM dev_machine_agent_runs r WHERE r.task_id=t.id AND r.machine_id=m.id
       )))`

const taskResourceCleanupKey = `'task-resource-cleanup:' || t.id || ':' || t.generation`

// QueueTaskResourceCleanup also finds tasks that finished before the cleanup
// operation was introduced. Give exhausted operations time to recover.
func (r *DevMachineRepository) QueueTaskResourceCleanup(ctx context.Context, now time.Time, limit int) (int64, error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO dev_machine_operations
 (workspace_id,machine_id,checkout_id,agent_task_id,action,status,generation,idempotency_key)
 SELECT t.workspace_id, m.id, c.id, t.id, 'cleanup_task', 'pending', m.generation, `+taskResourceCleanupKey+taskResourceCleanupCandidates+`
   AND NOT EXISTS (SELECT 1 FROM dev_machine_operations prior
     WHERE prior.machine_id=m.id AND prior.idempotency_key=(`+taskResourceCleanupKey+`)
       AND (prior.status NOT IN ('failed','cancelled') OR prior.updated_at > $1::timestamptz - interval '1 hour'))
 ORDER BY t.updated_at, t.id LIMIT $3 FOR UPDATE OF t SKIP LOCKED
 ON CONFLICT (machine_id,idempotency_key) DO UPDATE SET
   status='pending',attempts=0,available_at=now(),completed_at=NULL,error_code=NULL,error_message=NULL
 WHERE dev_machine_operations.status IN ('failed','cancelled')`, now, uuid.Nil, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// BeginTaskResourceCleanup rechecks eligibility when the operation runs.
// Retiring the checkout under lock prevents new previews, checks and terminal
// sessions from using a worktree that may already be partly removed on retry.
func (r *DevMachineRepository) BeginTaskResourceCleanup(ctx context.Context, op domain.DevMachineOperation, now time.Time) (*domain.DevMachineCheckout, []uuid.UUID, error) {
	if op.AgentTaskID == nil || op.CheckoutID == nil || op.LeaseOwner == nil {
		return nil, nil, ErrAgentTaskConflict
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var id uuid.UUID
	err = tx.GetContext(ctx, &id, `SELECT id FROM agent_tasks WHERE id=$1 AND workspace_id=$2 AND machine_id=$3 AND checkout_id=$4 FOR UPDATE`,
		op.AgentTaskID, op.WorkspaceID, op.MachineID, op.CheckoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	err = tx.GetContext(ctx, &id, `SELECT id FROM dev_machine_checkouts WHERE id=$1 AND workspace_id=$2 AND machine_id=$3 AND scope_key=$4 FOR UPDATE`,
		op.CheckoutID, op.WorkspaceID, op.MachineID, op.AgentTaskID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var checkout domain.DevMachineCheckout
	err = tx.GetContext(ctx, &checkout, `SELECT c.*`+taskResourceCleanupCandidates+`
   AND t.id=$3 AND c.id=$4 AND (`+taskResourceCleanupKey+`)=$5
   AND EXISTS (SELECT 1 FROM dev_machine_operations o WHERE o.id=$2 AND o.action='cleanup_task'
     AND o.status='leased' AND o.lease_owner=$6 AND o.lease_expires_at>now())`,
		now, op.ID, op.AgentTaskID, op.CheckoutID, op.IdempotencyKey, op.LeaseOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var runs []uuid.UUID
	err = tx.SelectContext(ctx, &runs, `SELECT id FROM dev_machine_agent_runs WHERE task_id=$1 AND workspace_id=$2 AND machine_id=$3 ORDER BY id`,
		op.AgentTaskID, op.WorkspaceID, op.MachineID)
	if err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dev_machine_checkouts SET status='retired',last_error=NULL WHERE id=$1 AND status<>'retired'`, checkout.ID); err != nil {
		return nil, nil, err
	}
	checkout.Status = "retired"
	return &checkout, runs, tx.Commit()
}
