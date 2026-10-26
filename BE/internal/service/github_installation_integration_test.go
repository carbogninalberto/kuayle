package service

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// With a shared app every workspace can name any installation of it, so the
// callback must prove the installing user can access the installation before
// binding it (and its private repositories) to the caller's workspace.
func TestGlobalInstallationCallbackRequiresInstallingUserPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Claimer','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Claimer',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })

	svc := NewGitHubService(repository.NewGitHubRepository(db), nil, nil, nil, nil, nil, nil, "https://kuayle.test", "",
		&GlobalGitHubAppConfig{AppID: 1, ClientID: "client-id", ClientSecret: "client-secret", WebhookSecret: "secret"})
	var checked []string
	svc.installationAccess = func(_ context.Context, code string, installationID int64) (bool, error) {
		checked = append(checked, code)
		require.EqualValues(t, 4242, installationID)
		return false, nil
	}

	for _, code := range []string{"", "code-for-another-account"} {
		_, err := svc.HandleInstallationCallback(ctx, ws, owner, 4242, code)
		require.ErrorIs(t, err, ErrGitHubInstallationUnverified)
	}
	require.Equal(t, []string{"code-for-another-account"}, checked, "a missing code must be refused before any lookup")
	var bound int
	require.NoError(t, db.Get(&bound, `SELECT COUNT(*) FROM github_installations WHERE workspace_id=$1`, ws))
	require.Zero(t, bound)
}
