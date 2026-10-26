package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConversationsPersistAuthorizeAndSerialize(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db := isolatedPrivacyDatabase(t, url)
	user, other, ws := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other} {
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Chat','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
	}
	_, err := db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Chat',$2,$3)`, ws, ws.String(), user)
	require.NoError(t, err)
	for _, id := range []uuid.UUID{user, other} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, ws, id)
		require.NoError(t, err)
	}
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: user})
	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: other})
	repo := NewConversationRepository(db)
	conversation, err := repo.Create(ctx, ws, "A conversation")
	require.NoError(t, err)
	_, err = repo.History(outsider, ws, conversation.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.History(context.Background(), ws, conversation.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	requestID := uuid.New()
	replies := make(chan uuid.UUID, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			reply, e := repo.Send(ctx, ws, conversation.ID, requestID, "Hello", "compatible", "test-model")
			if e != nil {
				t.Error(e)
				return
			}
			replies <- reply.ID
		})
	}
	wg.Wait()
	close(replies)
	var first uuid.UUID
	for id := range replies {
		if first == uuid.Nil {
			first = id
		}
		require.Equal(t, first, id)
	}
	history, err := repo.History(ctx, ws, conversation.ID, 0)
	require.NoError(t, err)
	require.Len(t, history.Messages, 2)
	_, err = repo.Send(ctx, ws, conversation.ID, requestID, "Different request", "compatible", "model")
	require.ErrorIs(t, err, ErrConversationConflict)
	_, err = repo.Send(ctx, ws, conversation.ID, uuid.New(), "Second", "compatible", "model")
	require.ErrorIs(t, err, ErrConversationBusy)
	attempt, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, attempt)
	duplicate, err := repo.Claim(context.Background())
	require.NoError(t, err)
	require.Nil(t, duplicate)
	event, err := repo.Append(ctx, *attempt, "Streaming text")
	require.NoError(t, err)
	require.Greater(t, event.Cursor, history.Cursor)
	require.NoError(t, repo.Finish(ctx, *attempt, "completed"))
	history, err = NewConversationRepository(db).History(ctx, ws, conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "Streaming text", history.Messages[1].Content)
	require.Equal(t, "completed", history.Messages[1].Status)
	events, err := repo.Events(ctx, ws, conversation.ID, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 4)
	for i := 1; i < len(events); i++ {
		require.Greater(t, events[i].Cursor, events[i-1].Cursor)
	}
	_, err = repo.Append(ctx, *attempt, "late output")
	require.ErrorIs(t, err, ErrConversationConflict)
	_, err = repo.Retry(ctx, ws, conversation.ID, attempt.Message.ID, uuid.New(), "compatible", "model")
	require.ErrorIs(t, err, ErrConversationConflict)
	_, err = repo.Send(ctx, ws, conversation.ID, uuid.New(), "Another turn", "compatible", "model")
	require.NoError(t, err)
	attempt, err = repo.Claim(context.Background())
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE conversation_messages SET lease_expires_at=$1 WHERE id=$2`, time.Now().Add(-time.Minute), attempt.Message.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Recover(context.Background()))
	history, err = repo.History(ctx, ws, conversation.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "interrupted", history.Messages[3].Status)
	_, err = repo.Append(ctx, *attempt, "stale worker")
	require.ErrorIs(t, err, ErrConversationConflict)
	_, err = repo.Retry(ctx, ws, conversation.ID, attempt.Message.ID, uuid.New(), "compatible", "model")
	require.NoError(t, err)
	attempt, err = repo.Claim(context.Background())
	require.NoError(t, err)
	require.NoError(t, repo.Cancel(ctx, ws, conversation.ID))
	_, err = repo.Append(ctx, *attempt, "cancelled output")
	require.ErrorIs(t, err, ErrConversationConflict)
	_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws, user)
	require.NoError(t, err)
	_, err = repo.History(ctx, ws, conversation.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Events(ctx, ws, conversation.ID, 0)
	require.ErrorIs(t, err, ErrConversationNotFound)
}
