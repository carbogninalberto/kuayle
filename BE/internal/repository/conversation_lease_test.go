package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A reply may run for ten minutes, longer than its lease: the live worker
// renews the lease, while a stale or expired attempt cannot.
func TestConversationsRenewLiveAttemptLeases(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, ws := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Lease','hash')`, user, user.String()+"@test.invalid")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Lease',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, user)
	require.NoError(t, err)
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	repo := NewConversationRepository(db)
	conversation, err := repo.Create(ctx, ws, "Long reply")
	require.NoError(t, err)
	_, err = repo.Send(ctx, ws, conversation.ID, uuid.New(), "Plan everything", "compatible", "model")
	require.NoError(t, err)
	attempt, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, attempt)
	expiry := func() time.Time {
		var at time.Time
		require.NoError(t, db.Get(&at, `SELECT lease_expires_at FROM conversation_messages WHERE id=$1`, attempt.Message.ID))
		return at
	}
	// Simulate a reply that has already run most of its lease.
	_, err = db.Exec(`UPDATE conversation_messages SET lease_expires_at=now()+interval '5 seconds' WHERE id=$1`, attempt.Message.ID)
	require.NoError(t, err)
	require.NoError(t, repo.RenewAttempt(ctx, *attempt))
	require.Greater(t, time.Until(expiry()), time.Minute, "renewal extends the lease")
	require.NoError(t, repo.Recover(context.Background()))
	history, err := repo.History(ctx, ws, conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "streaming", history.Messages[1].Status, "a renewed reply is not reclaimed")

	stale := *attempt
	stale.Message.LeaseToken = func() *uuid.UUID { id := uuid.New(); return &id }()
	require.ErrorIs(t, repo.RenewAttempt(ctx, stale), ErrConversationConflict, "another lease holder cannot renew")

	_, err = db.Exec(`UPDATE conversation_messages SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, attempt.Message.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.RenewAttempt(ctx, *attempt), ErrConversationConflict, "an expired lease is never revived")
	require.NoError(t, repo.Recover(context.Background()))
	history, err = repo.History(ctx, ws, conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "interrupted", history.Messages[1].Status)
}
