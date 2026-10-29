package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// Members read the assistant's tools; owners and admins switch them; guests,
// outsiders and personal access tokens are refused.
func TestAssistantCapabilitiesHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	owner, admin, member, guest, outsider, ws := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Tools','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Tools',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	workspaces := repository.NewWorkspaceRepository(db)
	issues, teams := repository.NewIssueRepository(db), repository.NewTeamRepository(db)
	conversationSvc := service.NewConversationService(repository.NewConversationRepository(db), nil)
	tools := service.NewAssistantTools(issues, teams, workspaces, nil)
	tools.SetSettings(repository.NewAssistantToolSettingsRepository(db))
	conversationSvc.SetTools(tools)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "capabilities-http-test-secret-at-least-32"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, &appHandlers{conversation: handler.NewConversationHandler(conversationSvc, "https://kuayle.test")}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Tools denied", Scopes: []string{"issues:read", "workspaces:read"}, TokenHash: hash, TokenPrefix: prefix}))
	root := server.URL + "/api/workspaces/" + ws.String()
	do := func(method, path, body, auth string) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, e := http.NewRequest(method, root+path, reader)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(res.Body).Decode(&payload)
		return res.StatusCode, payload
	}
	enabled := func(payload map[string]any, name string) any {
		for _, tool := range payload["tools"].([]any) {
			if entry := tool.(map[string]any); entry["name"] == name {
				return entry["enabled"]
			}
		}
		return nil
	}

	status, payload := do("GET", "/assistant-capabilities", "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, false, payload["can_manage"])
	require.Equal(t, true, enabled(payload, "create_issue"))
	for _, auth := range []string{tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("GET", "/assistant-capabilities", "", auth)))
	}

	off := `{"enabled":false}`
	for _, auth := range []string{tokenFor(member), tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("PUT", "/assistant-capabilities/create_issue", off, auth)))
	}
	require.Equal(t, 404, first(do("PUT", "/assistant-capabilities/no_such_tool", off, tokenFor(admin))))
	require.Equal(t, 400, first(do("PUT", "/assistant-capabilities/create_issue", `{"enabled":"no"}`, tokenFor(admin))))
	require.Equal(t, 400, first(do("PUT", "/assistant-capabilities/create_issue", `{}`, tokenFor(admin))), "enabled is required")
	status, payload = do("PUT", "/assistant-capabilities/create_issue", off, tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, false, payload["enabled"])
	status, payload = do("GET", "/assistant-capabilities", "", tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, true, payload["can_manage"])
	require.Equal(t, false, enabled(payload, "create_issue"), "members and owners see the admin's switch")
	require.Equal(t, 200, first(do("PUT", "/assistant-capabilities/create_issue", `{"enabled":true}`, tokenFor(owner))))
}
