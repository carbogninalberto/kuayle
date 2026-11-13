package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func forgeWebhookRouter() *echo.Echo {
	e := echo.New()
	e.POST("/api/forges/webhook/:connectionId", NewForgeHandler(nil).HandleWebhook)
	return e
}

// An unknown or malformed connection ID cannot select a secret, so the public
// webhook answers 401 without reading the payload.
func TestForgeWebhookRejectsUnknownConnection(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/forges/webhook/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	forgeWebhookRouter().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// The public webhook bounds its body before verification, like GitHub's.
func TestForgeWebhookBoundsUnauthenticatedBody(t *testing.T) {
	body := &endlessBody{}
	req := httptest.NewRequest(http.MethodPost, "/api/forges/webhook/00000000-0000-0000-0000-000000000001", io.NopCloser(body))
	rec := httptest.NewRecorder()
	forgeWebhookRouter().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.LessOrEqual(t, body.read, int64(maxForgeWebhookBytes+64*1024))
}

// The webhook maps verification and delivery failures to 401/400 and a valid
// delivery to 200, moving the linked issue with the shared automation rules.
func TestForgeWebhookHTTPStatusPostgres(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	owner, ws, team, issue := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Forge handler','hash')`, owner, owner.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Forge handler',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key) VALUES($1,$2,'Engineering','ENG')`, team, ws)
	require.NoError(t, err)
	var todo uuid.UUID
	for i, s := range []struct{ slug, category string }{
		{"backlog", "backlog"}, {"todo", "unstarted"}, {"in_progress", "started"},
		{"in_review", "started"}, {"done", "completed"}, {"cancelled", "cancelled"},
	} {
		_, err = db.Exec(`INSERT INTO team_statuses(team_id,name,slug,category,position,is_default) VALUES($1,$2,$3,$4,$5,$6)`,
			team, s.slug, s.slug, s.category, i, i == 0)
		require.NoError(t, err)
		if s.slug == "todo" {
			require.NoError(t, db.Get(&todo, `SELECT id FROM team_statuses WHERE team_id=$1 AND slug='todo'`, team))
		}
	}
	_, err = db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status,status_id)
		VALUES($1,$2,$3,1,'KUA-1','Forge issue',$4,'todo',$5)`, issue, ws, team, owner, todo)
	require.NoError(t, err)

	svc := service.NewForgeService(repository.NewForgeRepository(db), repository.NewGitHubRepository(db),
		repository.NewIssueRepository(db), repository.NewTeamRepository(db),
		repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db),
		crypto.DeriveKey("forge-handler-test"), nil, "https://kuayle.test", false)
	connection, err := svc.CreateConnection(ctx, ws, owner, dto.CreateForgeConnectionRequest{
		Provider: "gitlab", BaseURL: "https://gitlab.example.com", WebhookSecret: "secret",
	})
	require.NoError(t, err)
	connectionID := uuid.MustParse(connection.ID)
	_, err = svc.LinkRepo(ctx, ws, connectionID, dto.LinkForgeRepoRequest{ExternalRepoID: 42, FullName: "team/app", DefaultBranch: "main"})
	require.NoError(t, err)

	e := echo.New()
	e.POST("/api/forges/webhook/:connectionId", NewForgeHandler(svc).HandleWebhook)
	body := []byte(`{"object_kind":"merge_request","user":{"username":"alice"},"project":{"id":42,"path_with_namespace":"team/app","web_url":"https://gitlab.example.com/team/app"},"object_attributes":{"id":900,"iid":7,"title":"KUA-1 fix","description":"","state":"opened","action":"open","source_branch":"feat/kua-1-x","target_branch":"main","url":"https://gitlab.example.com/team/app/-/merge_requests/7"}}`)
	deliver := func(token, delivery string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/forges/webhook/"+connection.ID, bytes.NewReader(body))
		req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
		req.Header.Set("X-Gitlab-Token", token)
		if delivery != "" {
			req.Header.Set("X-Gitlab-Event-UUID", delivery)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusUnauthorized, deliver("wrong", uuid.NewString()))
	require.Equal(t, http.StatusBadRequest, deliver("secret", ""))
	require.Equal(t, http.StatusOK, deliver("secret", uuid.NewString()))
	var status string
	require.NoError(t, db.Get(&status, `SELECT status FROM issues WHERE id=$1`, issue))
	require.Equal(t, "in_review", status)
}
