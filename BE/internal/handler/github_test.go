package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endlessBody counts bytes consumed from an unauthenticated request body.
type endlessBody struct{ read int64 }

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	b.read += int64(len(p))
	return len(p), nil
}

// The webhook is public and reads its body before the signature check, so the
// body must be bounded rather than buffered without limit.
func TestGitHubWebhookBoundsUnauthenticatedBody(t *testing.T) {
	body := &endlessBody{}
	req := httptest.NewRequest(http.MethodPost, "/api/github/webhook", io.NopCloser(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	svc := service.NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", &service.GlobalGitHubAppConfig{AppID: 1, WebhookSecret: "secret"})

	require.NoError(t, NewGitHubHandler(svc).HandleWebhook(c))

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.LessOrEqual(t, body.read, int64(maxGitHubWebhookBytes+64*1024))
}

func signedGitHubWebhook(secret, event, delivery string, body []byte) *http.Request {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/api/github/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	return req
}

// GitHub sends X-GitHub-Delivery with every delivery; without it a delivery
// cannot be told apart from a replay, so even a correctly signed one is refused.
func TestGitHubWebhookRejectsSignedDeliveryWithoutDeliveryID(t *testing.T) {
	svc := service.NewGitHubService(nil, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", &service.GlobalGitHubAppConfig{AppID: 1, WebhookSecret: "fixture"})
	for name, delivery := range map[string]string{"missing": "", "oversized": strings.Repeat("a", 129), "control characters": "abc\tdef"} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := echo.New().NewContext(signedGitHubWebhook("fixture", "ping", delivery, []byte(`{"zen":"Keep it simple"}`)), rec)
			require.NoError(t, NewGitHubHandler(svc).HandleWebhook(c))
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// A captured delivery keeps its valid signature forever. Replaying the
// "installation deleted" delivery after the workspace reconnected must not
// disconnect it again: the second copy is acknowledged and not processed.
func TestGitHubWebhookProcessesADeliveryOncePostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	owner, ws := uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Webhook replay','hash')`, owner, owner.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Webhook replay',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	repo := repository.NewGitHubRepository(db)
	installationID := time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM github_webhook_deliveries WHERE installation_id=$1`, installationID)
	})
	connect := func() {
		t.Helper()
		require.NoError(t, repo.CreateInstallation(ctx, &domain.GitHubInstallation{ID: uuid.New(), WorkspaceID: ws, InstallationID: installationID, AccountLogin: "acme", AccountType: "Organization", InstalledBy: owner}))
	}
	connected := func() bool {
		t.Helper()
		inst, err := repo.GetInstallationByGitHubID(ctx, installationID)
		require.NoError(t, err)
		return inst != nil
	}
	h := NewGitHubHandler(service.NewGitHubService(repo, nil, nil, nil, nil, nil, nil, "https://kuayle.test", "", &service.GlobalGitHubAppConfig{AppID: 1, WebhookSecret: "fixture"}))
	body, err := json.Marshal(map[string]any{"action": "deleted", "installation": map[string]any{"id": installationID}})
	require.NoError(t, err)
	deliver := func(delivery string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		require.NoError(t, h.HandleWebhook(echo.New().NewContext(signedGitHubWebhook("fixture", "installation", delivery, body), rec)))
		return rec.Code
	}
	first, second := uuid.NewString(), uuid.NewString()

	connect()
	require.Equal(t, http.StatusOK, deliver(first))
	require.False(t, connected(), "the delivery is processed the first time")

	connect()
	require.Equal(t, http.StatusOK, deliver(first), "a duplicate is acknowledged so GitHub does not report a failure")
	require.True(t, connected(), "a replayed delivery must not be processed again")

	require.Equal(t, http.StatusOK, deliver(second))
	require.False(t, connected(), "a new delivery of the same event is still processed")
}
