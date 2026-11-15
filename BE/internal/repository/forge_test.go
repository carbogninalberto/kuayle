package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestForgeConnectionsAreWorkspaceScopedPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, mine, theirs := uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Forge repo','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	for _, ws := range []uuid.UUID{mine, theirs} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Forge repo',$2,$3)`, ws, ws.String(), owner)
		require.NoError(t, err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id IN ($1,$2)`, mine, theirs) })
	repo := NewForgeRepository(db)
	conn := &domain.ForgeConnection{ID: uuid.New(), WorkspaceID: mine, Provider: domain.ForgeGitLab, Name: "gitlab", BaseURL: "https://gitlab.example.com", WebhookSecret: "encrypted", CreatedBy: owner}
	require.NoError(t, repo.CreateConnection(ctx, conn))

	got, err := repo.GetConnectionByWorkspace(ctx, mine, conn.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	got, err = repo.GetConnectionByWorkspace(ctx, theirs, conn.ID)
	require.NoError(t, err)
	require.Nil(t, got, "another workspace cannot read the connection")
	got, err = repo.GetConnection(ctx, conn.ID)
	require.NoError(t, err)
	require.NotNil(t, got, "the public webhook path resolves by ID before verification")

	conn.Name = "renamed"
	conn.WorkspaceID = theirs
	require.ErrorIs(t, repo.UpdateConnection(ctx, conn), sql.ErrNoRows)
	require.ErrorIs(t, repo.DeleteConnection(ctx, theirs, conn.ID), sql.ErrNoRows)

	linked := &domain.ForgeRepo{ID: uuid.New(), ConnectionID: conn.ID, WorkspaceID: mine, ExternalRepoID: 42, FullName: "team/app", WebURL: "https://gitlab.example.com/team/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.LinkRepo(ctx, linked))
	relinked := &domain.ForgeRepo{ID: uuid.New(), ConnectionID: conn.ID, WorkspaceID: mine, ExternalRepoID: 42, FullName: "team/app-renamed", DefaultBranch: "trunk", IsActive: true}
	require.NoError(t, repo.LinkRepo(ctx, relinked))
	require.Equal(t, linked.ID, relinked.ID, "relinking returns the stored repository, not the discarded new ID")
	require.Equal(t, "team/app-renamed", relinked.FullName)
	wrong := &domain.ForgeRepo{ID: uuid.New(), ConnectionID: conn.ID, WorkspaceID: theirs, ExternalRepoID: 43, FullName: "team/other", DefaultBranch: "main", IsActive: true}
	require.ErrorIs(t, repo.LinkRepo(ctx, wrong), sql.ErrNoRows, "a repository cannot be linked into another workspace")
	repos, err := repo.ListReposByConnection(ctx, mine, conn.ID)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.ErrorIs(t, repo.DeleteRepo(ctx, mine, uuid.New(), linked.ID), sql.ErrNoRows)
}

func TestForgeUpsertsReportInsertedPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Forge upsert','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Forge upsert',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	repo := NewForgeRepository(db)
	conn := &domain.ForgeConnection{ID: uuid.New(), WorkspaceID: ws, Provider: domain.ForgeGitea, Name: "gitea", BaseURL: "https://gitea.example.com", WebhookSecret: "encrypted", CreatedBy: owner}
	require.NoError(t, repo.CreateConnection(ctx, conn))
	linked := &domain.ForgeRepo{ID: uuid.New(), ConnectionID: conn.ID, WorkspaceID: ws, ExternalRepoID: 42, FullName: "team/app", DefaultBranch: "main", IsActive: true}
	require.NoError(t, repo.LinkRepo(ctx, linked))

	mr := &domain.ForgeMergeRequestModel{ID: uuid.New(), WorkspaceID: ws, ForgeRepoID: linked.ID, ExternalID: 7, Number: 7, Title: "KUA-1", State: "open", AuthorLogin: "alice", WebURL: "https://gitea.example.com/team/app/pulls/7"}
	inserted, err := repo.UpsertMergeRequest(ctx, mr)
	require.NoError(t, err)
	require.True(t, inserted)
	mr.Title = "KUA-1 renamed"
	inserted, err = repo.UpsertMergeRequest(ctx, mr)
	require.NoError(t, err)
	require.False(t, inserted)
	require.NotZero(t, mr.CreatedAt)

	branch := &domain.ForgeBranchModel{ID: uuid.New(), WorkspaceID: ws, ForgeRepoID: linked.ID, Name: "feat/kua-1"}
	require.NoError(t, repo.UpsertBranch(ctx, branch))
	require.NoError(t, repo.UpsertBranch(ctx, branch))

	commit := &domain.ForgeCommitModel{ID: uuid.New(), WorkspaceID: ws, ForgeRepoID: linked.ID, SHA: "abc123", Message: "KUA-1", WebURL: "https://gitea.example.com/commit/abc123", CommittedAt: time.Now()}
	require.NoError(t, repo.UpsertCommit(ctx, commit))
	require.NoError(t, repo.UpsertCommit(ctx, commit))

	claimed, err := repo.ClaimWebhookDelivery(ctx, conn.ID, "delivery-1", time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimWebhookDelivery(ctx, conn.ID, "delivery-1", time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "a live claim cannot be processed twice")
	require.NoError(t, repo.CompleteWebhookDelivery(ctx, conn.ID, "delivery-1"))
	claimed, err = repo.ClaimWebhookDelivery(ctx, conn.ID, "delivery-1", time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "a processed delivery is refused")
	claimed, err = repo.ClaimWebhookDelivery(ctx, conn.ID, "delivery-2", time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repo.ReleaseWebhookDelivery(ctx, conn.ID, "delivery-2"))
	claimed, err = repo.ClaimWebhookDelivery(ctx, conn.ID, "delivery-2", time.Minute)
	require.NoError(t, err)
	require.True(t, claimed, "a failed delivery stays redeliverable")
}

func TestForgeMissingConnectionIsNotAnErrorPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	repo := NewForgeRepository(db)
	conn, err := repo.GetConnection(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Nil(t, conn)
	require.False(t, errors.Is(err, sql.ErrNoRows))
}
