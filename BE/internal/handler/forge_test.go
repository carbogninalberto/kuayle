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
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
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

type forgeHandlerFixture struct {
	db    *sqlx.DB
	svc   *service.ForgeService
	owner uuid.UUID
	ws    uuid.UUID
	issue uuid.UUID
	team  uuid.UUID
}

func newForgeHandlerFixture(t *testing.T) *forgeHandlerFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := repository.Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
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
		crypto.DeriveKey("forge-handler-test"), nil, "https://kuayle.test", "", false)
	return &forgeHandlerFixture{db: db, svc: svc, owner: owner, ws: ws, issue: issue, team: team}
}

// router mounts the configuration routes with the workspace/user middleware
// already applied, so the handlers can be exercised without the full server.
func (f *forgeHandlerFixture) router() *echo.Echo {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Set("workspace", &domain.Workspace{ID: f.ws})
			c.Set("user_id", f.owner)
			return next(c)
		}
	})
	h := NewForgeHandler(f.svc)
	e.POST("/forges/connections", h.CreateConnection)
	e.PATCH("/forges/connections/:id", h.UpdateConnection)
	e.DELETE("/forges/connections/:id", h.DeleteConnection)
	e.POST("/forges/connections/:id/repos", h.LinkRepo)
	e.DELETE("/forges/connections/:id/repos/:repoId", h.UnlinkRepo)
	return e
}

func (f *forgeHandlerFixture) do(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// The webhook maps verification failures to 401, a valid delivery without an ID
// header to 200 using the body hash, and a replay of that body to a no-op.
func TestForgeWebhookHTTPStatusPostgres(t *testing.T) {
	f := newForgeHandlerFixture(t)
	ctx := context.Background()
	connection, err := f.svc.CreateConnection(ctx, f.ws, f.owner, dto.CreateForgeConnectionRequest{
		Provider: "gitlab", BaseURL: "https://gitlab.example.com", WebhookSecret: "secret",
	})
	require.NoError(t, err)
	connectionID := uuid.MustParse(connection.ID)
	_, err = f.svc.LinkRepo(ctx, f.ws, connectionID, dto.LinkForgeRepoRequest{ExternalRepoID: 42, FullName: "team/app", DefaultBranch: "main"})
	require.NoError(t, err)

	e := echo.New()
	e.POST("/api/forges/webhook/:connectionId", NewForgeHandler(f.svc).HandleWebhook)
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
	status := func() string {
		t.Helper()
		var value string
		require.NoError(t, f.db.Get(&value, `SELECT status FROM issues WHERE id=$1`, f.issue))
		return value
	}

	require.Equal(t, http.StatusUnauthorized, deliver("wrong", uuid.NewString()))
	require.Equal(t, http.StatusOK, deliver("secret", ""))
	require.Equal(t, "in_review", status())

	_, err = f.db.Exec(`UPDATE issues SET status='todo' WHERE id=$1`, f.issue)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, deliver("secret", ""))
	require.Equal(t, "todo", status(), "an identical body is ignored as a replay")
}

// Validation errors return 400, duplicates and in-use base URLs return 409 and
// no response echoes driver text.
func TestForgeConfigErrorMappingPostgres(t *testing.T) {
	f := newForgeHandlerFixture(t)
	e := f.router()
	connection, err := f.svc.CreateConnection(context.Background(), f.ws, f.owner, dto.CreateForgeConnectionRequest{
		Provider: "gitlab", BaseURL: "https://gitlab.example.com", WebhookSecret: "secret",
	})
	require.NoError(t, err)

	invalid := f.do(t, e, http.MethodPost, "/forges/connections", `{"provider":"bitbucket","base_url":"https://gitlab.example.com","webhook_secret":"secret"}`)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Contains(t, invalid.Body.String(), "provider must be")
	require.NotContains(t, invalid.Body.String(), "SQLSTATE")
	require.NotContains(t, invalid.Body.String(), "pgx")

	duplicate := f.do(t, e, http.MethodPost, "/forges/connections", `{"provider":"gitlab","base_url":"https://gitlab.example.com","webhook_secret":"secret"}`)
	require.Equal(t, http.StatusConflict, duplicate.Code)
	require.Contains(t, duplicate.Body.String(), "CONFLICT")
	require.NotContains(t, duplicate.Body.String(), "23505")

	unsafe := f.do(t, e, http.MethodPost, "/forges/connections/"+connection.ID+"/repos",
		`{"external_repo_id":42,"full_name":"team/app","web_url":"javascript:alert(1)"}`)
	require.Equal(t, http.StatusBadRequest, unsafe.Code)
	require.Contains(t, unsafe.Body.String(), "web_url")

	_, err = f.svc.LinkRepo(context.Background(), f.ws, uuid.MustParse(connection.ID), dto.LinkForgeRepoRequest{
		ExternalRepoID: 42, FullName: "team/app", WebURL: "https://gitlab.example.com/team/app",
	})
	require.NoError(t, err)
	inUse := f.do(t, e, http.MethodPatch, "/forges/connections/"+connection.ID,
		`{"base_url":"https://gitlab-other.example.com"}`)
	require.Equal(t, http.StatusConflict, inUse.Code)
	require.Contains(t, inUse.Body.String(), "Unlink repositories")

	missing := f.do(t, e, http.MethodPatch, "/forges/connections/"+uuid.NewString(), `{"name":"x"}`)
	require.Equal(t, http.StatusNotFound, missing.Code)
}

// A repository failure becomes 500 with a generic message; the driver text and
// connection details stay server-side.
func TestForgeConfigInternalErrorHidesDriverText(t *testing.T) {
	db, err := sqlx.Open("pgx", "postgres://user:pass@127.0.0.1:1/kuayle?sslmode=disable&connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	svc := service.NewForgeService(repository.NewForgeRepository(db), repository.NewGitHubRepository(db),
		repository.NewIssueRepository(db), repository.NewTeamRepository(db),
		repository.NewTeamStatusRepository(db), repository.NewIssueHistoryRepository(db),
		crypto.DeriveKey("forge-handler-test"), nil, "https://kuayle.test", "", false)
	f := &forgeHandlerFixture{db: db, svc: svc, owner: uuid.New(), ws: uuid.New()}
	response := f.do(t, f.router(), http.MethodPost, "/forges/connections",
		`{"provider":"gitlab","base_url":"https://gitlab.example.com","webhook_secret":"secret"}`)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "An internal error occurred")
	require.NotContains(t, response.Body.String(), "127.0.0.1")
	require.NotContains(t, response.Body.String(), "connection refused")
	require.NotContains(t, response.Body.String(), "pgx")
}
