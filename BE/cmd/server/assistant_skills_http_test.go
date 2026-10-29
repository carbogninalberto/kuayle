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

// Members read the skills they can see; owners and admins write, version,
// activate and delete them; guests, outsiders and personal access tokens are
// refused; private-team skills stay hidden from non-members of the team.
func TestAssistantSkillsHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	owner, admin, member, guest, outsider, ws, private := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, admin, member, guest, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Skills','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Skills',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO teams(id,workspace_id,name,key,is_private) VALUES($1,$2,'Secret','SEC',true)`, private, ws)
	require.NoError(t, err)
	workspaces := repository.NewWorkspaceRepository(db)
	skills := service.NewAssistantSkillService(repository.NewAssistantSkillRepository(db), workspaces)

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "skills-http-test-secret-at-least-32-chars"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, &appHandlers{assistantSkill: handler.NewAssistantSkillHandler(skills)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Skills denied", Scopes: []string{"issues:read", "workspaces:read"}, TokenHash: hash, TokenPrefix: prefix}))
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
	names := func(payload map[string]any) []string {
		result := []string{}
		for _, skill := range payload["skills"].([]any) {
			result = append(result, skill.(map[string]any)["name"].(string))
		}
		return result
	}

	release := `{"name":"How we release","description":"Use when releasing","body":"1. Tag\n2. Ship"}`
	for _, auth := range []string{tokenFor(member), tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("POST", "/assistant-skills", release, auth)))
	}
	status, created := do("POST", "/assistant-skills", release, tokenFor(admin))
	require.Equal(t, 201, status)
	require.Equal(t, "active", created["status"], "an admin's skill is active unless drafted")
	id := created["id"].(string)
	require.Equal(t, 409, first(do("POST", "/assistant-skills", release, tokenFor(owner))))
	require.Equal(t, 400, first(do("POST", "/assistant-skills", `{"name":"","description":"d","body":"b"}`, tokenFor(owner))))
	require.Equal(t, 400, first(do("POST", "/assistant-skills", `{"name":"x","description":"d","body":"`+strings.Repeat("a", 65537)+`"}`, tokenFor(owner))), "bodies are capped at 64 KiB")
	require.Equal(t, 400, first(do("POST", "/assistant-skills", `{"name":"x","description":"`+strings.Repeat("a", 501)+`","body":"b"}`, tokenFor(owner))), "descriptions are capped at 500 characters")
	require.Equal(t, 400, first(do("POST", "/assistant-skills", `{"name":"x","description":"d","body":"b","extra":1}`, tokenFor(owner))))
	status, _ = do("POST", "/assistant-skills", `{"name":"Secret steps","description":"Team only","body":"hidden","team_id":"`+private.String()+`"}`, tokenFor(owner))
	require.Equal(t, 201, status)

	status, listed := do("GET", "/assistant-skills", "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, false, listed["can_manage"])
	require.Equal(t, []string{"How we release"}, names(listed), "private-team skills are hidden from non-members")
	status, listed = do("GET", "/assistant-skills", "", tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, true, listed["can_manage"])
	require.Equal(t, []string{"How we release", "Secret steps"}, names(listed))
	for _, auth := range []string{tokenFor(guest), tokenFor(outsider), raw} {
		require.Equal(t, 403, first(do("GET", "/assistant-skills", "", auth)))
		require.Equal(t, 403, first(do("GET", "/assistant-skills/"+id, "", auth)))
	}
	status, got := do("GET", "/assistant-skills/"+id, "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, "1. Tag\n2. Ship", got["body"])
	require.Equal(t, 404, first(do("GET", "/assistant-skills/"+uuid.NewString(), "", tokenFor(member))))

	// Edits are versions; a draft edit waits for activation.
	draft := `{"name":"How we release","description":"Use when releasing","body":"1. Tag\n2. Ship\n3. Announce","status":"draft"}`
	for _, auth := range []string{tokenFor(member), raw} {
		require.Equal(t, 403, first(do("PUT", "/assistant-skills/"+id, draft, auth)))
		require.Equal(t, 403, first(do("POST", "/assistant-skills/"+id+"/activate", "", auth)))
		require.Equal(t, 403, first(do("PUT", "/assistant-skills/"+id+"/enabled", `{"enabled":false}`, auth)))
		require.Equal(t, 403, first(do("POST", "/assistant-skills/"+id+"/versions/1/restore", "", auth)))
		require.Equal(t, 403, first(do("DELETE", "/assistant-skills/"+id, "", auth)))
	}
	status, updated := do("PUT", "/assistant-skills/"+id, draft, tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, float64(1), updated["current_version"])
	require.Equal(t, float64(2), updated["pending_version"])
	status, activated := do("POST", "/assistant-skills/"+id+"/activate", "", tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, float64(2), activated["current_version"])
	require.Equal(t, "1. Tag\n2. Ship\n3. Announce", activated["body"])
	require.Equal(t, 409, first(do("POST", "/assistant-skills/"+id+"/activate", `{"version":1}`, tokenFor(admin))))
	status, restored := do("POST", "/assistant-skills/"+id+"/versions/1/restore", "", tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, float64(3), restored["current_version"])
	require.Equal(t, "1. Tag\n2. Ship", restored["body"])
	require.Equal(t, 404, first(do("POST", "/assistant-skills/"+id+"/versions/9/restore", "", tokenFor(owner))))
	status, versions := do("GET", "/assistant-skills/"+id+"/versions", "", tokenFor(member))
	require.Equal(t, 200, status)
	require.Len(t, versions["versions"], 3)

	require.Equal(t, 400, first(do("PUT", "/assistant-skills/"+id+"/enabled", `{}`, tokenFor(admin))))
	status, disabled := do("PUT", "/assistant-skills/"+id+"/enabled", `{"enabled":false}`, tokenFor(admin))
	require.Equal(t, 200, status)
	require.Equal(t, false, disabled["enabled"])

	require.Equal(t, 204, first(do("DELETE", "/assistant-skills/"+id, "", tokenFor(owner))))
	require.Equal(t, 404, first(do("GET", "/assistant-skills/"+id, "", tokenFor(owner))))
}
