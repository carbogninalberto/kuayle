package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// An owner of one workspace must not delete another workspace's linked
// repository (and its cascaded pull requests and commits) by ID.
func TestUnlinkRepoIsWorkspaceScopedPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	repo := repository.NewGitHubRepository(db)

	linkRepo := func(name string) (uuid.UUID, *domain.GitHubRepoModel) {
		owner, ws := uuid.New(), uuid.New()
		_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,$3,'hash')`, owner, owner.String()+"@test.invalid", name)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,$2,$3,$4)`, ws, name, ws.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
		inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: time.Now().UnixNano(), AccountLogin: name, AccountType: "User", InstalledBy: owner}
		require.NoError(t, repo.CreateInstallation(ctx, inst))
		linked := &domain.GitHubRepoModel{ID: uuid.New(), WorkspaceID: ws, InstallationID: inst.ID, GitHubRepoID: 7, FullName: name + "/repo", DefaultBranch: "main", IsActive: true}
		require.NoError(t, repo.CreateRepo(ctx, linked))
		return ws, linked
	}
	attackerWorkspace, attackerRepo := linkRepo("attacker")
	_, victimRepo := linkRepo("victim")

	svc := NewGitHubService(repo, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)

	require.ErrorIs(t, svc.UnlinkRepo(ctx, attackerWorkspace, victimRepo.ID), ErrGitHubRepoNotFound)
	var remaining int
	require.NoError(t, db.Get(&remaining, `SELECT COUNT(*) FROM github_repos WHERE id=$1`, victimRepo.ID))
	require.Equal(t, 1, remaining, "another workspace's repository must survive")

	require.NoError(t, svc.UnlinkRepo(ctx, attackerWorkspace, attackerRepo.ID))
	require.NoError(t, db.Get(&remaining, `SELECT COUNT(*) FROM github_repos WHERE id=$1`, attackerRepo.ID))
	require.Zero(t, remaining)
}
