package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Only the task's creator can have their terminal sign-in captured, and only
// while the task is stopped for it; a new attempt clears the request.
func TestSignInCaptureIsLimitedToTheCreatorOfATaskStoppedForSignIn(t *testing.T) {
	db, repo, ctx, task, claimed := publishingTask(t)
	_, err := db.Exec(`UPDATE dev_machines SET status='running' WHERE id=$1`, *claimed.MachineID)
	require.NoError(t, err)
	_, err = repo.RequestSignInCapture(ctx, task.WorkspaceID, task.ID, uuid.New())
	require.ErrorIs(t, err, ErrAgentTaskConflict, "the task is not stopped for sign-in")

	require.NoError(t, repo.FailForSignIn(ctx, *claimed, "Codex needs you to sign in"))
	current, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelling", current.State, "the attempt is cleaned up like any failure")
	require.True(t, current.FailurePending)
	// Runtime cleanup confirmed, as FinishCancellation records after the cancel operation.
	_, err = db.Exec(`UPDATE agent_tasks SET state='failed',failure_pending=false,lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, task.ID)
	require.NoError(t, err)
	current, err = repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State)
	require.True(t, current.SignInRequired)

	other := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.RequestSignInCapture(other, task.WorkspaceID, task.ID, uuid.New())
	require.Error(t, err, "another user cannot capture the creator's sign-in")
	_, err = repo.RequestSignInCapture(ctx, task.WorkspaceID, task.ID, uuid.Nil)
	require.ErrorIs(t, err, ErrConversationInvalid)

	request := uuid.New()
	op, err := repo.RequestSignInCapture(ctx, task.WorkspaceID, task.ID, request)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineOpCaptureSignIn, op.Action)
	require.Equal(t, task.RunID, *op.AgentRunID)
	require.Equal(t, task.CreatorID, *op.RequestedByUserID)
	again, err := repo.RequestSignInCapture(ctx, task.WorkspaceID, task.ID, request)
	require.NoError(t, err)
	require.Equal(t, op.ID, again.ID, "a double click converges on one capture")

	_, err = db.Exec(`UPDATE dev_machine_operations SET status='completed',lease_owner=NULL,lease_expires_at=NULL WHERE machine_id=$1`, *claimed.MachineID)
	require.NoError(t, err)
	retried, err := repo.Retry(ctx, task.WorkspaceID, task.ID, domain.AgentTaskRetryRequest{RequestID: uuid.New(), Generation: current.Generation, Prompt: current.Prompt})
	require.NoError(t, err)
	require.False(t, retried.SignInRequired, "a new attempt starts without the sign-in request")

	store := NewDevMachineRepository(db)
	encrypted, err := store.GetAgentSignIn(ctx, task.WorkspaceID, task.CreatorID, "codex")
	require.NoError(t, err)
	require.Empty(t, encrypted)
	require.NoError(t, store.SaveAgentSignIn(ctx, task.WorkspaceID, task.CreatorID, "codex", "sealed-1"))
	require.NoError(t, store.SaveAgentSignIn(ctx, task.WorkspaceID, task.CreatorID, "codex", "sealed-2"))
	encrypted, err = store.GetAgentSignIn(ctx, task.WorkspaceID, task.CreatorID, "codex")
	require.NoError(t, err)
	require.Equal(t, "sealed-2", encrypted, "a refreshed sign-in replaces the old one")
	require.Error(t, store.SaveAgentSignIn(ctx, task.WorkspaceID, task.CreatorID, "goose", "sealed"), "only agents with a sign-in file")
}
