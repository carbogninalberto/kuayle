package repository

import (
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPushSubscriptionLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not configured")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	owner, other, workspace := uuid.New(), uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{owner, other} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Member','hash')`, user, user.String()+"@test.invalid")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,owner_id,name,slug) VALUES($1,$2,'Workspace',$3)`, workspace, owner, workspace.String())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2),($1,$3)`, workspace, owner, other)
	require.NoError(t, err)

	repo := NewPushSubscriptionRepository(db)
	sub := domain.PushSubscription{UserID: owner, WorkspaceID: workspace, Endpoint: "https://push.example/" + uuid.NewString(),
		P256DH: "key-1", Auth: "auth", UserAgent: "browser"}
	sub.ID, err = repo.Register(ctx, sub, nil)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, sub.ID)
	stale := sub
	sub.P256DH = "key-2"
	id, err := repo.Register(ctx, sub, nil)
	require.NoError(t, err)
	require.Equal(t, sub.ID, id)
	intruder := sub
	intruder.UserID = other
	_, err = repo.Register(ctx, intruder, nil)
	require.ErrorIs(t, err, ErrPushSubscriptionConflict)
	intruder.Endpoint += "-replacement"
	_, err = repo.Register(ctx, intruder, &id)
	require.ErrorIs(t, err, ErrPushSubscriptionConflict)
	require.NoError(t, repo.Delete(ctx, other, id, nil))
	require.NoError(t, repo.Delete(ctx, owner, id, &workspace), "a workspace-scoped session cannot revoke a general enrollment")
	require.NoError(t, repo.RecordDelivery(ctx, stale, http.StatusGone), "a stale attempt cannot delete refreshed keys")
	require.NoError(t, repo.RecordDelivery(ctx, sub, http.StatusServiceUnavailable))
	rows, err := repo.ListByUser(ctx, owner)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "key-2", rows[0].P256DH)
	require.Equal(t, 1, rows[0].FailureCount)
	require.NoError(t, repo.RecordDelivery(ctx, sub, http.StatusCreated))
	rows, err = repo.ListByUser(ctx, owner)
	require.NoError(t, err)
	require.Zero(t, rows[0].FailureCount)
	require.NotNil(t, rows[0].LastSuccessAt)

	sub.Endpoint += "-new"
	sub.ID, err = repo.Register(ctx, sub, &id)
	require.NoError(t, err)
	require.NotEqual(t, id, sub.ID)
	rows, err = repo.ListByUser(ctx, owner)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sub.ID, rows[0].ID)
	require.NoError(t, repo.RecordDelivery(ctx, sub, http.StatusGone))
	rows, err = repo.ListByUser(ctx, owner)
	require.NoError(t, err)
	require.Empty(t, rows)

	sub.SSOScoped = true
	id, err = repo.Register(ctx, sub, nil)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, owner, id, &workspace))
	rows, err = repo.ListByUser(ctx, owner)
	require.NoError(t, err)
	require.Empty(t, rows)
}
