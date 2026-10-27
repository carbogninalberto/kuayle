package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrSignInNotFound means the user has not finished signing in the agent in
// their machine's terminal yet.
var ErrSignInNotFound = errors.New("no agent sign-in was found in the machine's terminal")

var agentDisplayNames = map[string]string{"claude-code": "Claude Code", "codex": "Codex"}

// signInWait bounds how long continuing after sign-in waits for the manager
// to read the sign-in from the terminal.
const signInWait = 25 * time.Second

// ContinueAfterSignIn stores the sign-in the task's creator completed in their
// machine's terminal and starts a new attempt. requestID makes a double click
// converge on one capture and one retry.
func (s *AgentTaskService) ContinueAfterSignIn(ctx context.Context, ws, id, requestID uuid.UUID) (*domain.AgentTask, error) {
	op, err := s.Repo.RequestSignInCapture(ctx, ws, id, requestID)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(signInWait)
	for op.Status != domain.DevMachineOpStatusCompleted {
		if op.Status == domain.DevMachineOpStatusFailed || op.Status == domain.DevMachineOpStatusCancelled {
			return nil, ErrSignInNotFound
		}
		if time.Now().After(deadline) {
			return nil, context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		current, err := s.machines.store.GetOperationByIdempotency(ctx, ws, op.MachineID, op.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if current != nil {
			op = current
		}
	}
	task, err := s.Repo.Get(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	// The new attempt keeps the task's own instructions and test override.
	var command []string
	if task.TestCommand != nil {
		_ = json.Unmarshal(*task.TestCommand, &command)
	}
	return s.Retry(ctx, ws, id, domain.AgentTaskRetryRequest{RequestID: requestID, Generation: task.Generation, Prompt: task.Prompt, TestCommand: command})
}
