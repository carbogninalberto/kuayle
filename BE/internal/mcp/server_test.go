package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// fakeAPI stands in for the application router: it records the request a tool
// call turns into and answers with a canned response.
type fakeAPI struct {
	requests []*http.Request
	bodies   []string
	status   int
	response string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, r)
	f.bodies = append(f.bodies, string(body))
	if r.URL.Path == "/api/workspaces" {
		_, _ = w.Write([]byte(`[{"slug":"solo"}]`))
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_, _ = w.Write([]byte(f.response))
}

type rpcResult struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError  bool `json:"isError"`
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"contents"`
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"inputSchema"`
			Annotations struct {
				ReadOnlyHint bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
		ProtocolVersion string `json:"protocolVersion"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

// post sends one message to an MCP endpoint whose caller holds a personal
// access token, unless session is set.
func post(t *testing.T, api http.Handler, target, message string, session bool) (*httptest.ResponseRecorder, rpcResult) {
	t.Helper()
	e := echo.New()
	e.POST("/mcp", NewServer(api).Handle, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !session {
				c.Set(middleware.TokenScopesKey, []string{"issues:read"})
			}
			return next(c)
		}
	})
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(message))
	req.Header.Set("Authorization", "Bearer kuayle_pat_example")
	req.Header.Set("Cookie", "access_token=session")
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var out rpcResult
	if rec.Body.Len() > 0 && rec.Code != http.StatusUnauthorized {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	}
	return rec, out
}

func call(t *testing.T, api http.Handler, target, tool, arguments string) rpcResult {
	t.Helper()
	rec, out := post(t, api, target, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+arguments+`}}`, false)
	require.Equal(t, http.StatusOK, rec.Code)
	return out
}

func TestInitializeAndToolList(t *testing.T) {
	rec, out := post(t, &fakeAPI{}, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, false)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "2025-06-18", out.Result.ProtocolVersion)

	rec, _ = post(t, &fakeAPI{}, "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, false)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Zero(t, rec.Body.Len())

	_, out = post(t, &fakeAPI{}, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, false)
	readOnly := map[string]bool{}
	for _, tool := range out.Result.Tools {
		readOnly[tool.Name] = tool.Annotations.ReadOnlyHint
		if tool.Name == "create_issue" {
			require.ElementsMatch(t, []string{"title", "team_id"}, tool.InputSchema.Required)
			require.Contains(t, tool.InputSchema.Properties, "workspace")
		}
	}
	for _, name := range []string{"list_workspaces", "list_teams", "list_projects", "list_cycles", "list_labels", "list_statuses", "list_members", "list_issues", "get_issue", "list_comments", "search_wiki_pages", "get_wiki_page"} {
		require.True(t, readOnly[name], "%s must be listed as read-only", name)
	}
	for _, name := range []string{"create_issue", "update_issue", "add_comment", "create_wiki_page", "update_wiki_page"} {
		write, listed := readOnly[name]
		require.True(t, listed && !write, "%s must be listed as a write tool", name)
	}

	_, out = post(t, &fakeAPI{}, "/mcp", `{"jsonrpc":"2.0","id":3,"method":"nope"}`, false)
	require.Equal(t, codeMethodNotFound, out.Error.Code)
}

func TestSessionsAndMalformedMessagesAreRefused(t *testing.T) {
	api := &fakeAPI{}
	rec, _ := post(t, api, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, true)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	rec, out := post(t, api, "/mcp", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, false)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, codeParseError, out.Error.Code)
	require.Empty(t, api.requests)
}

func TestToolCallsBecomeAPIRequestsAsTheCaller(t *testing.T) {
	api := &fakeAPI{response: `{"identifier":"KUA-7"}`}
	out := call(t, api, "/mcp?workspace=acme", "update_issue", `{"identifier":"KUA-7","title":"New","priority":2,"label_ids":["a","b"],"due_date":""}`)
	require.False(t, out.Result.IsError)
	require.Equal(t, `{"identifier":"KUA-7"}`, out.Result.Content[0].Text)
	require.Len(t, api.requests, 1)
	sent := api.requests[0]
	require.Equal(t, http.MethodPatch, sent.Method)
	require.Equal(t, "/api/workspaces/acme/issues/KUA-7", sent.URL.Path)
	require.JSONEq(t, `{"title":"New","priority":2,"label_ids":["a","b"],"due_date":""}`, api.bodies[0])
	// The API sees the caller's token and address, and no browser session.
	require.Equal(t, "Bearer kuayle_pat_example", sent.Header.Get("Authorization"))
	require.Equal(t, "203.0.113.9:4000", sent.RemoteAddr)
	require.Empty(t, sent.Header.Get("Cookie"))

	// Filters of a read become the query string; the argument beats the URL.
	api = &fakeAPI{response: `{"data":[]}`}
	call(t, api, "/mcp?workspace=acme", "list_issues", `{"workspace":"other","search":"login bug","per_page":5,"assignee":null}`)
	require.Equal(t, "/api/workspaces/other/issues", api.requests[0].URL.Path)
	require.Equal(t, "per_page=5&search=login+bug", api.requests[0].URL.RawQuery)

	api = &fakeAPI{}
	call(t, api, "/mcp?workspace=acme", "update_wiki_page", `{"scope":"project","scope_id":"p1","page_id":"w1","version":3,"content":"# Hi"}`)
	require.Equal(t, "/api/workspaces/acme/projects/p1/wiki/w1", api.requests[0].URL.Path)
	require.JSONEq(t, `{"version":3,"content":"# Hi"}`, api.bodies[0])
}

func TestWorkspaceDefaultsToTheOnlyOneTheTokenReaches(t *testing.T) {
	api := &fakeAPI{response: `[]`}
	out := call(t, api, "/mcp", "list_teams", `{}`)
	require.False(t, out.Result.IsError)
	require.Len(t, api.requests, 2)
	require.Equal(t, "/api/workspaces/solo/teams", api.requests[1].URL.Path)
}

func TestAPIRefusalsAreToolErrors(t *testing.T) {
	api := &fakeAPI{status: http.StatusForbidden, response: `{"error":{"code":"FORBIDDEN","message":"You do not have permission to perform this action"}}`}
	out := call(t, api, "/mcp?workspace=acme", "create_issue", `{"title":"x","team_id":"t"}`)
	require.Nil(t, out.Error)
	require.True(t, out.Result.IsError)
	require.Contains(t, out.Result.Content[0].Text, "FORBIDDEN")
}

func TestArgumentsCannotLeaveTheirRoute(t *testing.T) {
	for name, arguments := range map[string]string{
		"path traversal":   `{"identifier":"../../members"}`,
		"encoded slash":    `{"identifier":"KUA-1%2Fcomments"}`,
		"query injection":  `{"identifier":"KUA-1?x=1"}`,
		"workspace escape": `{"workspace":"a/../b","identifier":"KUA-1"}`,
		"missing":          `{}`,
		"not a string":     `{"identifier":7}`,
		"unknown argument": `{"identifier":"KUA-1","sort_order":1}`,
	} {
		api := &fakeAPI{}
		out := call(t, api, "/mcp?workspace=acme", "get_issue", arguments)
		require.True(t, out.Result.IsError, name)
		require.Empty(t, api.requests, name)
	}
	api := &fakeAPI{}
	out := call(t, api, "/mcp?workspace=acme", "get_wiki_page", `{"scope":"issue","scope_id":"a","page_id":"b"}`)
	require.True(t, out.Result.IsError)
	require.Empty(t, api.requests)

	_, unknown := post(t, api, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_everything","arguments":{}}}`, false)
	require.Equal(t, codeInvalidParams, unknown.Error.Code)
}

func TestResources(t *testing.T) {
	read := func(api *fakeAPI, uri string) rpcResult {
		_, out := post(t, api, "/mcp?workspace=acme", `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"`+uri+`"}}`, false)
		return out
	}
	api := &fakeAPI{response: `{"identifier":"KUA-1"}`}
	out := read(api, "kuayle://issue/KUA-1")
	require.Nil(t, out.Error)
	require.Equal(t, "kuayle://issue/KUA-1", out.Result.Contents[0].URI)
	require.Equal(t, `{"identifier":"KUA-1"}`, out.Result.Contents[0].Text)
	require.Equal(t, http.MethodGet, api.requests[0].Method)
	require.Equal(t, "/api/workspaces/acme/issues/KUA-1", api.requests[0].URL.Path)

	api = &fakeAPI{response: `{}`}
	require.Nil(t, read(api, "kuayle://wiki/team/t1/w1").Error)
	require.Equal(t, "/api/workspaces/acme/teams/t1/wiki/w1", api.requests[0].URL.Path)

	// A hidden or missing issue is indistinguishable: not found.
	api = &fakeAPI{status: http.StatusNotFound, response: `{"error":{"code":"NOT_FOUND","message":"Issue not found"}}`}
	require.Equal(t, codeResourceNotFound, read(api, "kuayle://issue/KUA-2").Error.Code)

	api = &fakeAPI{}
	for _, uri := range []string{"kuayle://issue/KUA-1/comments", "kuayle://wiki/issue/t1/w1", "kuayle://team/t1", "https://example.com/"} {
		require.NotNil(t, read(api, uri).Error, uri)
	}
	require.Empty(t, api.requests)
}
