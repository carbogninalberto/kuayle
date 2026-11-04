package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type setupTransport func(*http.Request) (*http.Response, error)

func (f setupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setupResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func setupTestWorkspace(t *testing.T, db *sqlx.DB, repo *repository.GitHubRepository, installationID int64) uuid.UUID {
	t.Helper()
	owner, ws := uuid.New(), uuid.New()
	_, err := db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Setup','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Setup',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	if installationID > 0 {
		inst := &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: installationID, AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner}
		require.NoError(t, repo.CreateInstallation(context.Background(), inst))
	}
	return ws
}

func testAppPrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// The setup check reads only the caller workspace's installation, reports a
// clear state for every failure, and never surfaces GitHub's error details.
func TestGitHubAppSetupIsWorkspaceScopedPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	repo := repository.NewGitHubRepository(db)

	base := time.Now().UnixNano()
	mine, theirs := base, base+1
	myWorkspace := setupTestWorkspace(t, db, repo, mine)
	setupTestWorkspace(t, db, repo, theirs)
	notInstalled := setupTestWorkspace(t, db, repo, 0)

	events, permissions := grantAll()
	eventsJSON, _ := json.Marshal(events)
	permissionsJSON, _ := json.Marshal(permissions)
	var mu sync.Mutex
	var requested []string
	fail := ""
	svc := NewGitHubService(repo, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "",
		&GlobalGitHubAppConfig{AppID: 9, PrivateKey: testAppPrivateKey(t), Slug: "kuayle"})
	svc.githubHTTPClient = &http.Client{Transport: setupTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mode := fail
		mu.Unlock()
		switch mode {
		case "network":
			return nil, errors.New("dial tcp: connection refused")
		case "status":
			return setupResponse(502, `{"message":"secret-ish upstream detail"}`), nil
		case "uninstalled":
			if r.URL.Path != "/app" {
				return setupResponse(404, `{"message":"Not Found"}`), nil
			}
		}
		if r.URL.Path == "/app" {
			return setupResponse(200, `{"slug":"kuayle","owner":{"login":"kuayle-hq","type":"Organization"},
				"events":`+string(eventsJSON)+`,"permissions":`+string(permissionsJSON)+`}`), nil
		}
		return setupResponse(200, `{"id":1,"html_url":"https://github.com/organizations/acme/settings/installations/1",
			"events":["push"],"permissions":{"metadata":"read"}}`), nil
	})}

	got, err := svc.GetAppSetup(ctx, myWorkspace)
	require.NoError(t, err)
	require.Equal(t, dto.GitHubAppSetupInstallationPending, got.State)
	require.True(t, got.Managed, "a shared app is configured by the Kuayle operator")
	require.Contains(t, requested, "/app/installations/"+strconv.FormatInt(mine, 10))
	require.NotContains(t, requested, "/app/installations/"+strconv.FormatInt(theirs, 10), "another workspace's installation must not be read")

	got, err = svc.GetAppSetup(ctx, notInstalled)
	require.NoError(t, err)
	require.Equal(t, dto.GitHubAppSetupNotInstalled, got.State)

	for _, mode := range []string{"network", "status"} {
		mu.Lock()
		fail = mode
		mu.Unlock()
		got, err = svc.GetAppSetup(ctx, myWorkspace)
		require.NoError(t, err, mode)
		require.Equal(t, dto.GitHubAppSetupUnavailable, got.State, mode)
		require.Empty(t, got.AppSettingsURL)
	}

	// Uninstalled on GitHub while Kuayle still records the installation.
	mu.Lock()
	fail = "uninstalled"
	mu.Unlock()
	got, err = svc.GetAppSetup(ctx, myWorkspace)
	require.NoError(t, err)
	require.Equal(t, dto.GitHubAppSetupInstallationMissing, got.State)

	// A self-hosted workspace without its own app is not configured, even when
	// another workspace has an app and installation.
	selfHosted := NewGitHubService(repo, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", nil)
	got, err = selfHosted.GetAppSetup(ctx, myWorkspace)
	require.NoError(t, err)
	require.Equal(t, dto.GitHubAppSetupNotConfigured, got.State)
}
