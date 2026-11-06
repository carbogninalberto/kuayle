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
	"github.com/kuayle/kuayle-backend/pkg/crypto"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// Members read the research settings without the key; owners and admins
// change them; guests, outsiders and personal access tokens are refused.
func TestAssistantResearchSettingsHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	owner, admin, member, guest, outsider, ws := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Research','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Research',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	workspaces := repository.NewWorkspaceRepository(db)
	research := service.NewAssistantResearchService(repository.NewAssistantResearchSettingsRepository(db), workspaces, crypto.DeriveKey("research-http-test:assistant-research"))

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "research-http-test-secret-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, &appHandlers{assistantResearch: handler.NewAssistantResearchHandler(research)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Research denied", Scopes: []string{"issues:read", "workspaces:read"}, TokenHash: hash, TokenPrefix: prefix}))
	url := server.URL + "/api/workspaces/" + ws.String() + "/assistant-research-settings"
	const key = "BSA-very-secret-search-key"
	do := func(method, body, auth string) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, e := http.NewRequest(method, url, reader)
		require.NoError(t, e)
		req.Header.Set("Authorization", "Bearer "+auth)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		require.NotContains(t, string(data), key, "the key never leaves the server")
		var payload map[string]any
		_ = json.Unmarshal(data, &payload)
		return res.StatusCode, payload
	}

	status, payload := do("GET", "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, false, payload["can_manage"])
	require.Equal(t, false, payload["has_search_api_key"])
	require.Equal(t, "brave", payload["search_provider"])
	for _, auth := range []string{tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("GET", "", auth)))
	}
	update := `{"search_provider":"tavily","search_api_key":"` + key + `","subagent_model":"small","subagent_max_parallel":2}`
	for _, auth := range []string{tokenFor(member), tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("PUT", update, auth)))
	}
	require.Equal(t, 400, first(do("PUT", `{"search_provider":"google"}`, tokenFor(admin))))
	require.Equal(t, 400, first(do("PUT", `{"unknown":1}`, tokenFor(admin))), "unknown fields are refused")
	status, payload = do("PUT", update, tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, true, payload["has_search_api_key"])
	require.Equal(t, "tavily", payload["search_provider"])
	require.EqualValues(t, 2, payload["subagent_max_parallel"])

	var stored string
	require.NoError(t, db.Get(&stored, `SELECT search_api_key_encrypted FROM assistant_research_settings WHERE workspace_id=$1`, ws))
	require.NotContains(t, stored, key, "stored encrypted")
	config, err := research.ResearchConfig(context.Background(), ws)
	require.NoError(t, err)
	require.Equal(t, key, config.SearchAPIKey)

	status, payload = do("GET", "", tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, true, payload["can_manage"])
	require.Equal(t, true, payload["has_search_api_key"])
	status, payload = do("PUT", `{"search_api_key":null}`, tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, false, payload["has_search_api_key"])
	require.Equal(t, "tavily", payload["search_provider"], "other settings are kept")
}
