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
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/kuayle/kuayle-backend/pkg/storage"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// Members read the bot settings; only workspace managers change them, and
// tokens without the manage scope cannot. Invalid names and non-image uploads
// are refused with their code; the uploaded picture is public, as GitHub must
// load it, and stops being served once replaced.
func TestGitHubBotHTTP(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	defer db.Close()
	owner, member, guest, ws := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, member, guest} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Bot','hash')`, id, id.String()+"@test.invalid")
		require.NoError(t, err)
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Bot',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	defer db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws)
	for id, role := range map[uuid.UUID]string{owner: "owner", member: "member", guest: "guest"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, id, role)
		require.NoError(t, err)
	}
	files, err := storage.NewLocalBackend(t.TempDir(), "")
	require.NoError(t, err)
	botSvc := service.NewGitHubBotService(repository.NewGitHubBotRepository(db), files, "https://kuayle.test")

	tokens := repository.NewPersonalAccessTokenRepository(db)
	secret := "github-bot-http-test-secret-32-chars-min"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(repository.NewWorkspaceRepository(db))
	e := echo.New()
	registerRoutes(e, &appHandlers{githubBot: handler.NewGitHubBotHandler(botSvc)}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()
	tokenFor := func(id uuid.UUID) string {
		token, e := jwtpkg.GenerateAccessToken(id, secret)
		require.NoError(t, e)
		return token
	}
	raw, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: owner, Name: "Bot read", Scopes: []string{"issues:read"}, TokenHash: hash, TokenPrefix: prefix}))
	root := server.URL + "/api/workspaces/" + ws.String()
	do := func(method, url, contentType string, body io.Reader, auth string) (int, map[string]any) {
		t.Helper()
		req, e := http.NewRequest(method, url, body)
		require.NoError(t, e)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res, e := http.DefaultClient.Do(req)
		require.NoError(t, e)
		defer res.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(res.Body).Decode(&payload)
		return res.StatusCode, payload
	}
	upload := func(data []byte, auth string) (int, map[string]any) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, e := form.CreateFormFile("file", "bot.png")
		require.NoError(t, e)
		_, _ = part.Write(data)
		require.NoError(t, form.Close())
		return do("POST", root+"/github/bot/avatar", form.FormDataContentType(), &body, auth)
	}
	errorCode := func(payload map[string]any) string {
		return payload["error"].(map[string]any)["code"].(string)
	}

	status, payload := do("GET", root+"/github/bot", "", nil, tokenFor(member))
	require.Equal(t, 200, status)
	require.Equal(t, "Kuayle", payload["display_name"])
	require.Equal(t, 403, first(do("GET", root+"/github/bot", "", nil, tokenFor(guest))))

	rename := `{"display_name":"Acme Reviewer","abilities":{"mention_reviews":true}}`
	require.Equal(t, 403, first(do("PATCH", root+"/github/bot", "application/json", strings.NewReader(rename), tokenFor(member))))
	require.Equal(t, 403, first(do("PATCH", root+"/github/bot", "application/json", strings.NewReader(rename), raw)))
	status, payload = do("PATCH", root+"/github/bot", "application/json", strings.NewReader(`{"display_name":"@everyone"}`), tokenFor(owner))
	require.Equal(t, 400, status)
	require.Equal(t, "INVALID_NAME", errorCode(payload))
	status, payload = do("PATCH", root+"/github/bot", "application/json", strings.NewReader(rename), tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, "Acme Reviewer", payload["display_name"])

	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	require.Equal(t, 403, first(upload(png, tokenFor(member))))
	status, payload = upload([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), tokenFor(owner))
	require.Equal(t, 400, status)
	require.Equal(t, "UNSUPPORTED_IMAGE", errorCode(payload))
	require.Equal(t, 413, first(upload(append(png, make([]byte, domain.GitHubBotAvatarMaxBytes)...), tokenFor(owner))))
	status, payload = upload(png, tokenFor(owner))
	require.Equal(t, 200, status)
	require.Equal(t, "upload", payload["avatar_source"])
	firstAvatar := payload["avatar_url"].(string)

	res, err := http.Get(server.URL + firstAvatar)
	require.NoError(t, err)
	served, _ := io.ReadAll(res.Body)
	res.Body.Close()
	require.Equal(t, 200, res.StatusCode, "the picture is public so GitHub can load it")
	require.Equal(t, "image/png", res.Header.Get("Content-Type"))
	require.Equal(t, png, served)

	_, payload = upload(png, tokenFor(owner))
	require.NotEqual(t, firstAvatar, payload["avatar_url"])
	require.Equal(t, 404, first(do("GET", server.URL+firstAvatar, "", nil, "")), "a replaced picture is no longer served")
	other := strings.Replace(payload["avatar_url"].(string), ws.String(), uuid.NewString(), 1)
	require.Equal(t, 404, first(do("GET", server.URL+other, "", nil, "")))
}
