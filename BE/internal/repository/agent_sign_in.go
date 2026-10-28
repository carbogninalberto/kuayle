package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// RequestSignInCapture queues reading the agent's sign-in from the task
// creator's terminal on the task's machine. Only the creator may do this, and
// only while the task is stopped for sign-in, so nobody can capture another
// user's login. requestID makes repeated clicks after a failed capture new
// operations while a double click converges on one.
func (r *AgentTaskRepository) RequestSignInCapture(ctx context.Context, ws, id, requestID uuid.UUID) (*domain.DevMachineOperation, error) {
	actor, err := conversationActor(ctx, ws)
	if err != nil {
		return nil, err
	}
	if requestID == uuid.Nil {
		return nil, ErrConversationInvalid
	}
	tx, task, err := r.transaction(ctx, id, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if task.CreatorID != actor {
		return nil, ErrAgentTaskUnauthorized
	}
	if task.State != "failed" || !task.SignInRequired || task.MachineID == nil {
		return nil, ErrAgentTaskConflict
	}
	var generation int64
	err = tx.GetContext(ctx, &generation, `SELECT m.generation FROM dev_machines m JOIN dev_machine_agent_runs r ON r.machine_id=m.id AND r.id=$3
 WHERE m.id=$1 AND m.workspace_id=$2 AND m.status='running'`, task.MachineID, task.WorkspaceID, task.RunID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentTaskConflict
	}
	if err != nil {
		return nil, err
	}
	key := "sign-in:" + task.RunID.String() + ":" + requestID.String()
	var existing domain.DevMachineOperation
	err = tx.GetContext(ctx, &existing, `SELECT * FROM dev_machine_operations WHERE workspace_id=$1 AND machine_id=$2 AND idempotency_key=$3`, task.WorkspaceID, *task.MachineID, key)
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	op := &domain.DevMachineOperation{
		WorkspaceID: task.WorkspaceID, MachineID: *task.MachineID, AgentRunID: &task.RunID,
		Action: domain.DevMachineOpCaptureSignIn, Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: key, RequestedByUserID: &actor, MaxAttempts: 1,
	}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return nil, err
	}
	return op, tx.Commit()
}
