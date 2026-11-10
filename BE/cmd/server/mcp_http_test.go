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
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The MCP endpoint is limited before it authenticates, so a client is slowed
// down whether or not its token is valid.
func TestMCPIsRateLimited(t *testing.T) {
	middleware := testMiddleware(mw.Auth("mcp-rate-limit-test-secret", nil))
	middleware.mcpRateLimit = mw.RateLimit(0.001, 2)
	e := echo.New()
	registerRoutes(e, &appHandlers{}, middleware)
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}
	require.Equal(t, http.StatusUnauthorized, post().Code)
	require.Equal(t, http.StatusUnauthorized, post().Code)
	limited := post()
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.NotEmpty(t, limited.Header().Get("Retry-After"))

	// The transport's optional stream is not offered.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// MCP over real HTTP, the production route manifest, PAT auth, workspace and
// permission middleware and PostgreSQL: tools act as the token's user, within
// the token's scopes and that user's view of private teams.
func TestMCPToolsFollowTokenScopesAndTeamPrivacy(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	owner, member := uuid.New(), uuid.New()
	ws := uuid.New()
	for _, id := range []uuid.UUID{owner, member} {
		_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'MCP tester','hash')`, id, id.String()+"@example.test")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	}
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'MCP',$2,$3)`, ws, ws.String(), owner)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	for user, role := range map[uuid.UUID]string{owner: "owner", member: "member"} {
		_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3)`, ws, user, role)
		require.NoError(t, err)
	}

	users := repository.NewUserRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	teams := repository.NewTeamRepository(db)
	statuses := repository.NewTeamStatusRepository(db)
	issues := repository.NewIssueRepository(db)
	projects := repository.NewProjectRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	notifications := service.NewNotificationService(repository.NewNotificationRepository(db))
	issueService := service.NewIssueService(issues, teams, statuses, repository.NewIssueHistoryRepository(db), hub, notifications, projects)
	commentService := service.NewCommentService(repository.NewCommentRepository(db), issues, hub, notifications)
	handlers := &appHandlers{
		workspace: handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, users, hub)),
		team:      handler.NewTeamHandler(service.NewTeamService(teams, statuses, hub)),
		issue:     handler.NewIssueHandler(issueService, commentService, users, statuses, projects, repository.NewCycleRepository(db), service.NewIssueRelationService(repository.NewIssueRelationRepository(db), issues)),
		wiki:      handler.NewWikiHandler(service.NewWikiService(repository.NewWikiRepository(db)), nil),
	}
	secret := "mcp-http-test-secret"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, handlers, middleware)
	server := httptest.NewServer(e)
	defer server.Close()

	pat := func(user uuid.UUID, scopes ...string) string {
		t.Helper()
		raw, hash, prefix, err := domain.GenerateToken()
		require.NoError(t, err)
		require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "MCP", TokenHash: hash, TokenPrefix: prefix, Scopes: pq.StringArray(scopes), WorkspaceSlugs: pq.StringArray{ws.String()}}))
		return raw
	}
	readScopes := []string{"workspaces:read", "teams:read", "issues:read", "comments:read", "wiki:read"}
	ownerWrite := pat(owner, append([]string{"issue:create", "issue:update", "wiki:write"}, readScopes...)...)
	ownerRead := pat(owner, readScopes...)
	// The outsider's token has every scope: only team membership hides the issue.
	outsider := pat(member, append([]string{"issue:create", "issue:update", "wiki:write"}, readScopes...)...)

	send := func(authorization, message string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader(message))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if authorization != "" {
			req.Header.Set("Authorization", "Bearer "+authorization)
		}
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, data
	}
	// tool calls a tool and returns its text and whether it is an error result.
	tool := func(token, name string, arguments map[string]any) (string, bool) {
		t.Helper()
		params, err := json.Marshal(map[string]any{"name": name, "arguments": arguments})
		require.NoError(t, err)
		status, data := send(token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`)
		require.Equal(t, http.StatusOK, status, "%s", data)
		var out struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(data, &out))
		require.Len(t, out.Result.Content, 1, "%s", data)
		return out.Result.Content[0].Text, out.Result.IsError
	}
	ok := func(token, name string, arguments map[string]any) string {
		t.Helper()
		text, failed := tool(token, name, arguments)
		require.False(t, failed, "%s: %s", name, text)
		return text
	}
	refused := func(token, name string, arguments map[string]any, code string) {
		t.Helper()
		text, failed := tool(token, name, arguments)
		require.True(t, failed, "%s: %s", name, text)
		require.Contains(t, text, code)
	}

	// Only a valid personal access token opens the endpoint.
	status, _ := send("", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = send(domain.TokenPrefix+"unknown", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusUnauthorized, status)
	session, err := jwtpkg.GenerateAccessToken(owner, secret)
	require.NoError(t, err)
	status, _ = send(session, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusUnauthorized, status)

	// A private team with one issue, set up through the API as the owner.
	api := func(method, path, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+"/api/workspaces/"+ws.String()+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "access_token", Value: session})
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, want, resp.StatusCode, "%s %s: %s", method, path, data)
		return data
	}
	var team struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(api("POST", "/teams", `{"name":"Secret team","key":"SEC","is_private":true,"acknowledge_privacy_limitations":true}`, http.StatusCreated), &team))

	// The workspace comes from the token's single workspace: no argument needed.
	require.Contains(t, ok(ownerWrite, "list_teams", nil), "Secret team")

	// A read-only token cannot call write tools, and nothing is written.
	refused(ownerRead, "create_issue", map[string]any{"title": "Must not exist", "team_id": team.ID}, "FORBIDDEN")
	var count int
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM issues WHERE workspace_id=$1`, ws))
	require.Zero(t, count)

	// Create and update act as the token's user and show in the issue history.
	var issue struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
		CreatorID  string `json:"creator_id"`
		Priority   int    `json:"priority"`
		Status     string `json:"status"`
	}
	require.NoError(t, json.Unmarshal([]byte(ok(ownerWrite, "create_issue", map[string]any{"title": "Confidential MCP issue", "team_id": team.ID, "description": "# Plan"})), &issue))
	require.Equal(t, "SEC-1", issue.Identifier)
	require.Equal(t, owner.String(), issue.CreatorID)
	require.NoError(t, json.Unmarshal([]byte(ok(ownerWrite, "update_issue", map[string]any{"identifier": "SEC-1", "priority": 1, "status": "in_progress", "assignee_ids": []string{owner.String()}, "due_date": "2030-01-31"})), &issue))
	require.Equal(t, 1, issue.Priority)
	require.Equal(t, "in_progress", issue.Status)
	var editors []string
	require.NoError(t, db.Select(&editors, `SELECT DISTINCT user_id::text FROM issue_history WHERE issue_id=$1 AND field IN ('priority','status')`, issue.ID))
	require.Equal(t, []string{owner.String()}, editors)
	// The API's validation applies unchanged.
	refused(ownerWrite, "update_issue", map[string]any{"identifier": "SEC-1", "status_id": uuid.NewString()}, "BAD_REQUEST")
	refused(ownerWrite, "create_issue", map[string]any{"team_id": team.ID}, "VALIDATION_ERROR")

	ok(ownerWrite, "add_comment", map[string]any{"identifier": "SEC-1", "body": "Commented over MCP"})
	var author string
	require.NoError(t, db.Get(&author, `SELECT user_id::text FROM comments WHERE issue_id=$1`, issue.ID))
	require.Equal(t, owner.String(), author)
	require.Contains(t, ok(ownerRead, "list_comments", map[string]any{"identifier": "SEC-1"}), "Commented over MCP")

	refused(ownerRead, "update_issue", map[string]any{"identifier": "SEC-1", "title": "Changed by a read-only token"}, "FORBIDDEN")
	refused(ownerRead, "add_comment", map[string]any{"identifier": "SEC-1", "body": "no"}, "FORBIDDEN")
	refused(ownerRead, "create_wiki_page", map[string]any{"scope": "team", "scope_id": team.ID, "title": "no"}, "FORBIDDEN")
	require.Contains(t, ok(ownerRead, "get_issue", map[string]any{"identifier": "SEC-1"}), "Confidential MCP issue")
	require.Contains(t, ok(ownerRead, "list_issues", map[string]any{"search": "Confidential", "team": team.ID}), "SEC-1")

	// Wiki pages: create, find by content, update with the read version.
	var page struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
		Content string `json:"content"`
	}
	wiki := map[string]any{"scope": "team", "scope_id": team.ID}
	require.NoError(t, json.Unmarshal([]byte(ok(ownerWrite, "create_wiki_page", map[string]any{"scope": "team", "scope_id": team.ID, "title": "Runbook", "content": "Restart the zeppelin service."})), &page))
	require.Contains(t, ok(ownerRead, "search_wiki_pages", map[string]any{"scope": "team", "scope_id": team.ID, "search": "ZEPPELIN"}), page.ID)
	require.NotContains(t, ok(ownerRead, "search_wiki_pages", map[string]any{"scope": "team", "scope_id": team.ID, "search": "100%_missing"}), page.ID)
	// The search reads the text of a page, not its markup: the content is stored
	// as HTML, so a tag name must not match every page.
	require.NotContains(t, ok(ownerRead, "search_wiki_pages", map[string]any{"scope": "team", "scope_id": team.ID, "search": "<p>"}), page.ID)
	require.NotContains(t, ok(ownerRead, "search_wiki_pages", map[string]any{"scope": "team", "scope_id": team.ID, "search": "/p"}), page.ID)
	require.Contains(t, ok(ownerRead, "search_wiki_pages", wiki), page.ID)
	ok(ownerWrite, "update_wiki_page", map[string]any{"scope": "team", "scope_id": team.ID, "page_id": page.ID, "version": page.Version, "content": "Restart twice."})
	refused(ownerWrite, "update_wiki_page", map[string]any{"scope": "team", "scope_id": team.ID, "page_id": page.ID, "version": page.Version, "content": "Stale edit."}, "WIKI_VERSION_CONFLICT")
	require.Contains(t, ok(ownerRead, "get_wiki_page", map[string]any{"scope": "team", "scope_id": team.ID, "page_id": page.ID}), "Restart twice.")

	// A workspace member outside the private team sees none of it over MCP.
	require.NotContains(t, ok(outsider, "list_teams", nil), "Secret team")
	require.NotContains(t, ok(outsider, "list_issues", nil), "SEC-1")
	require.NotContains(t, ok(outsider, "list_issues", map[string]any{"search": "Confidential"}), "Confidential MCP issue")
	refused(outsider, "get_issue", map[string]any{"identifier": "SEC-1"}, "NOT_FOUND")
	refused(outsider, "list_comments", map[string]any{"identifier": "SEC-1"}, "NOT_FOUND")
	refused(outsider, "update_issue", map[string]any{"identifier": "SEC-1", "title": "Taken over"}, "")
	refused(outsider, "add_comment", map[string]any{"identifier": "SEC-1", "body": "hello"}, "NOT_FOUND")
	refused(outsider, "create_issue", map[string]any{"title": "Planted", "team_id": team.ID}, "")
	refused(outsider, "search_wiki_pages", wiki, "")
	refused(outsider, "get_wiki_page", map[string]any{"scope": "team", "scope_id": team.ID, "page_id": page.ID}, "NOT_FOUND")
	var title string
	require.NoError(t, db.Get(&title, `SELECT title FROM issues WHERE id=$1`, issue.ID))
	require.Equal(t, "Confidential MCP issue", title)
	require.NoError(t, db.Get(&count, `SELECT COUNT(*) FROM issues WHERE workspace_id=$1`, ws))
	require.Equal(t, 1, count)

	// Resources follow the same rules.
	read := func(token, uri string) string {
		t.Helper()
		status, data := send(token, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"`+uri+`"}}`)
		require.Equal(t, http.StatusOK, status)
		return string(data)
	}
	require.Contains(t, read(ownerRead, "kuayle://issue/SEC-1"), "Confidential MCP issue")
	require.Contains(t, read(ownerRead, "kuayle://wiki/team/"+team.ID+"/"+page.ID), "Restart twice.")
	for _, uri := range []string{"kuayle://issue/SEC-1", "kuayle://wiki/team/" + team.ID + "/" + page.ID} {
		hidden := read(outsider, uri)
		require.Contains(t, hidden, `"code":-32002`)
		require.NotContains(t, hidden, "Confidential")
		require.NotContains(t, hidden, "Restart")
	}

	// A token restricted to this workspace cannot name another one.
	refused(ownerWrite, "list_teams", map[string]any{"workspace": "another-workspace"}, "")
}
