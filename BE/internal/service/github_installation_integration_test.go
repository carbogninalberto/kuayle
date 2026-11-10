package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

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

// GitHub lists an installation to every user who can reach one repository it
// covers. The callback must therefore bind a shared-app installation only for
// the account it is installed on or an admin of that organization, deciding
// from what GitHub reports for the installing user's OAuth grant.
func TestGlobalInstallationCallbackRequiresOwnerOrOrganizationAdminPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	repo := repository.NewGitHubRepository(db)
	appKey := testAppPrivateKey(t)
	base := time.Now().UnixNano()
	const githubUserID, organizationID = 501, 9001

	cases := []struct {
		name string
		// account is the installation's account as GitHub lists it.
		accountID   int64
		accountType string
		// membership is the status and body of GET /user/memberships/orgs/acme.
		membershipStatus int
		membership       string
		bound            bool
	}{
		{name: "organization member", accountID: organizationID, accountType: "Organization", membershipStatus: 200,
			membership: `{"role":"member","state":"active","organization":{"id":9001,"login":"acme"}}`},
		{name: "organization admin", accountID: organizationID, accountType: "Organization", membershipStatus: 200,
			membership: `{"role":"admin","state":"active","organization":{"id":9001,"login":"acme"}}`, bound: true},
		{name: "membership lookup forbidden", accountID: organizationID, accountType: "Organization", membershipStatus: 403,
			membership: `{"message":"Resource not accessible by integration"}`},
		{name: "own user account", accountID: githubUserID, accountType: "User", bound: true},
		{name: "another user's account", accountID: githubUserID + 1, accountType: "User"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner, ws := uuid.New(), uuid.New()
			_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Binder','hash')`, owner, owner.String()+"@test.invalid")
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
			_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Binder',$2,$3)`, ws, ws.String(), owner)
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })

			installationID := base + int64(i)
			account := fmt.Sprintf(`{"id":%d,"login":"acme","type":%q}`, tc.accountID, tc.accountType)
			var readAsApp atomic.Int32
			svc := NewGitHubService(repo, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "",
				&GlobalGitHubAppConfig{AppID: 9, PrivateKey: appKey, ClientID: "client-id", ClientSecret: "fixture", WebhookSecret: "fixture"})
			svc.githubHTTPClient = &http.Client{Transport: setupTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "github.com" && r.URL.Path == "/login/oauth/access_token" {
					return setupResponse(200, `{"access_token":"fixture","token_type":"bearer"}`), nil
				}
				if r.URL.Host != "api.github.com" {
					return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
				}
				switch r.URL.Path {
				case "/user/installations":
					return setupResponse(200, fmt.Sprintf(`{"installations":[{"id":%d,"target_type":%q,"account":%s}]}`, installationID, tc.accountType, account)), nil
				case "/user":
					return setupResponse(200, fmt.Sprintf(`{"id":%d,"login":"octo"}`, githubUserID)), nil
				case "/user/memberships/orgs/acme":
					return setupResponse(tc.membershipStatus, tc.membership), nil
				case fmt.Sprintf("/app/installations/%d", installationID):
					readAsApp.Add(1)
					return setupResponse(200, fmt.Sprintf(`{"id":%d,"account":%s}`, installationID, account)), nil
				}
				return setupResponse(404, `{"message":"Not Found"}`), nil
			})}

			inst, err := svc.HandleInstallationCallback(ctx, ws, owner, installationID, "oauth-code")

			var bound int
			require.NoError(t, db.Get(&bound, `SELECT COUNT(*) FROM github_installations WHERE workspace_id=$1 AND installation_id=$2`, ws, installationID))
			if !tc.bound {
				require.ErrorIs(t, err, ErrGitHubInstallationUnverified)
				require.Zero(t, bound, "a refused installation must not be bound")
				require.Zero(t, readAsApp.Load(), "a refused installation must not be read with the app's credentials")
				return
			}
			require.NoError(t, err)
			require.Equal(t, installationID, inst.InstallationID)
			require.Equal(t, "acme", inst.AccountLogin)
			require.Equal(t, 1, bound)
		})
	}
}
