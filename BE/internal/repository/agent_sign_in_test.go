package repository

import (
	"context"
	"testing"
	"time"

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

// A saved Claude Code token sits next to the captured file, is used only until
// its estimated expiry, is listed without its value, and each user sees and
// removes only their own sign-ins.
func TestAgentSignInTokensAreKeptPerUserNextToTheCapturedFile(t *testing.T) {
	db, _, ctx, task, _ := publishingTask(t)
	store := NewDevMachineRepository(db)
	ws, user := task.WorkspaceID, task.CreatorID
	other := uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','test')`, other, other.String()+"@example.test")
	require.NoError(t, err)

	require.NoError(t, store.SaveAgentSignIn(ctx, ws, user, "claude-code", "sealed-file"))
	token, err := store.GetAgentSignInToken(ctx, ws, user, "claude-code")
	require.NoError(t, err)
	require.Empty(t, token)
	expires := time.Now().Add(365 * 24 * time.Hour)
	require.NoError(t, store.SaveAgentSignInToken(ctx, ws, user, "claude-code", "sealed-token-1", expires))
	require.NoError(t, store.SaveAgentSignInToken(ctx, ws, user, "claude-code", "sealed-token-2", expires))
	require.Error(t, store.SaveAgentSignInToken(ctx, ws, user, "codex", "sealed", expires), "Codex has no token")
	token, err = store.GetAgentSignInToken(ctx, ws, user, "claude-code")
	require.NoError(t, err)
	require.Equal(t, "sealed-token-2", token, "a new token replaces the old one")
	file, err := store.GetAgentSignIn(ctx, ws, user, "claude-code")
	require.NoError(t, err)
	require.Equal(t, "sealed-file", file, "the captured file is kept")

	signIns, err := store.ListAgentSignIns(ctx, ws, user)
	require.NoError(t, err)
	require.Len(t, signIns, 2)
	for _, signIn := range signIns {
		require.Equal(t, "claude-code", signIn.Provider)
		if signIn.Kind == domain.AgentSignInToken {
			require.NotNil(t, signIn.ExpiresAt)
			require.WithinDuration(t, expires, *signIn.ExpiresAt, time.Second)
		} else {
			require.Equal(t, domain.AgentSignInCredentials, signIn.Kind)
			require.Nil(t, signIn.ExpiresAt)
		}
	}
	signIns, err = store.ListAgentSignIns(ctx, ws, other)
	require.NoError(t, err)
	require.Empty(t, signIns, "another user sees none of them")
	removed, err := store.DeleteAgentSignIns(ctx, ws, other, "claude-code", "")
	require.NoError(t, err)
	require.Zero(t, removed, "another user cannot remove them")

	_, err = db.Exec(`UPDATE agent_sign_ins SET expires_at=now()-interval '1 minute' WHERE kind='token'`)
	require.NoError(t, err)
	token, err = store.GetAgentSignInToken(ctx, ws, user, "claude-code")
	require.NoError(t, err)
	require.Empty(t, token, "an expired token is not used")

	removed, err = store.DeleteAgentSignIns(ctx, ws, user, "claude-code", domain.AgentSignInToken)
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)
	file, err = store.GetAgentSignIn(ctx, ws, user, "claude-code")
	require.NoError(t, err)
	require.Equal(t, "sealed-file", file, "removing the token keeps the terminal sign-in")
	removed, err = store.DeleteAgentSignIns(ctx, ws, user, "claude-code", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)
}
