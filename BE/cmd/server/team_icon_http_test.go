package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
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
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Team icons of a private team are served and changed only for people who can
// see the team, through the production routes, auth and PostgreSQL visibility.
func TestTeamIconHTTPPrivateTeam(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, teamMember, member, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ws, other := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, teamMember, member, outsider} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Team icon HTTP','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	for _, id := range []uuid.UUID{ws, other} {
		_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Team icon HTTP',$2,$3)`, id, id.String(), owner)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, id) })
		for user, role := range map[uuid.UUID]string{owner: "owner", teamMember: "member", member: "member"} {
			_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, id, user, role)
			require.NoError(t, err)
		}
	}
	dir := t.TempDir()
	store, err := storage.NewLocalBackend(dir, "/uploads")
	require.NoError(t, err)
	users := repository.NewUserRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	teamService := service.NewTeamService(repository.NewTeamRepository(db), repository.NewTeamStatusRepository(db), hub)
	teamService.SetIconStore(store)
	workspaceService := service.NewWorkspaceService(workspaces, users, hub)
	workspaceService.SetTeamIconStore(store)
	handlers := &appHandlers{workspace: handler.NewWorkspaceHandler(workspaceService), team: handler.NewTeamHandler(teamService)}
	secret := "team-icon-http-test-secret"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, handlers, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	jwt := func(user uuid.UUID) string {
		t.Helper()
		token, err := jwtpkg.GenerateAccessToken(user, secret)
		require.NoError(t, err)
		return token
	}
	ownerJWT, teamMemberJWT, memberJWT, outsiderJWT := jwt(owner), jwt(teamMember), jwt(member), jwt(outsider)
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: member, Name: "Team icon HTTP", TokenHash: hash, TokenPrefix: prefix, Scopes: pq.StringArray{"teams:read"}}))
	memberPAT := raw

	do := func(token string, req *http.Request, want int) []byte {
		t.Helper()
		if strings.HasPrefix(token, domain.TokenPrefix) {
			req.Header.Set("Authorization", "Bearer "+token)
		} else if token != "" {
			req.AddCookie(&http.Cookie{Name: "access_token", Value: token})
		}
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, want, resp.StatusCode, "%s %s: %s", req.Method, req.URL.Path, data)
		return data
	}
	request := func(token, method, path, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		return do(token, req, want)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	require.NoError(t, err)
	upload := func(token, path string, want int) []byte {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", "icon.png")
		require.NoError(t, err)
		_, err = part.Write(png)
		require.NoError(t, err)
		require.NoError(t, form.Close())
		req, err := http.NewRequest(http.MethodPost, server.URL+path, &body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", form.FormDataContentType())
		return do(token, req, want)
	}
	storedFiles := func() []string {
		t.Helper()
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}

	base := "/api/workspaces/" + ws.String()
	created := request(ownerJWT, "POST", base+"/teams", `{"name":"Hidden icon","key":"HID","is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusCreated)
	var team struct {
		ID   string  `json:"id"`
		Icon *string `json:"icon"`
	}
	require.NoError(t, json.Unmarshal(created, &team))
	teamPath := base + "/teams/" + team.ID
	request(ownerJWT, "POST", teamPath+"/members", `{"user_id":"`+teamMember.String()+`"}`, http.StatusOK)

	require.NoError(t, json.Unmarshal(upload(ownerJWT, teamPath+"/icon", http.StatusOK), &team))
	file, ok := domain.TeamIconImageFile(team.Icon)
	require.True(t, ok)
	iconPath := teamPath + "/icon/" + file
	require.Len(t, storedFiles(), 1)

	// A team member and the owner see the image.
	require.Equal(t, png, request(teamMemberJWT, "GET", iconPath, "", http.StatusOK))
	request(ownerJWT, "GET", iconPath, "", http.StatusOK)
	// A workspace member outside the private team does not, by session or token.
	request(memberJWT, "GET", iconPath, "", http.StatusNotFound)
	request(memberPAT, "GET", iconPath, "", http.StatusNotFound)
	// Nor does a non-member of the workspace.
	request(outsiderJWT, "GET", iconPath, "", http.StatusForbidden)
	// Neither the team member nor the outsider of the team may change it.
	upload(teamMemberJWT, teamPath+"/icon", http.StatusForbidden)
	upload(memberJWT, teamPath+"/icon", http.StatusForbidden)
	upload(outsiderJWT, teamPath+"/icon", http.StatusForbidden)

	// The team does not exist in another workspace, even for its owner.
	otherTeamPath := "/api/workspaces/" + other.String() + "/teams/" + team.ID
	request(ownerJWT, "GET", otherTeamPath+"/icon/"+file, "", http.StatusNotFound)
	upload(ownerJWT, otherTeamPath+"/icon", http.StatusNotFound)
	require.Len(t, storedFiles(), 1, "a refused upload stores nothing")

	// Losing team membership revokes access to the image.
	request(ownerJWT, "DELETE", teamPath+"/members/"+teamMember.String(), "", http.StatusOK)
	request(teamMemberJWT, "GET", iconPath, "", http.StatusNotFound)

	// Deleting the workspace removes its teams' icon files.
	request(ownerJWT, "DELETE", base, "", http.StatusOK)
	require.Empty(t, storedFiles())
}
