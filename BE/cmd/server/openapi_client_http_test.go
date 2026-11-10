package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// specClient is a client derived from the served OpenAPI document alone: it
// finds operations by operationId, builds the URL from the documented path,
// and checks every request and response body against the documented schema.
// If the document and the server disagree, a call fails.
type specClient struct {
	t          *testing.T
	doc        map[string]any
	operations map[string]specOperation
	baseURL    string
	token      string
	http       *http.Client
}

type specOperation struct {
	method, path string
	definition   map[string]any
}

func newSpecClient(t *testing.T, baseURL, token string, httpClient *http.Client) *specClient {
	t.Helper()
	resp, err := httpClient.Get(baseURL + "/api/openapi.json")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	client := &specClient{t: t, operations: map[string]specOperation{}, baseURL: baseURL, token: token, http: httpClient}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&client.doc))
	for path, item := range client.doc["paths"].(map[string]any) {
		for method, definition := range item.(map[string]any) {
			op := definition.(map[string]any)
			client.operations[op["operationId"].(string)] = specOperation{strings.ToUpper(method), path, op}
		}
	}
	return client
}

// call runs one documented operation with a request the document allows and
// returns the status and decoded body.
func (c *specClient) call(operationID string, pathParams map[string]string, query string, body any) (int, any) {
	c.t.Helper()
	return c.send(operationID, pathParams, query, body, true)
}

// send is call with the choice to skip the request check, for sending a body
// the document forbids and reading the error.
func (c *specClient) send(operationID string, pathParams map[string]string, query string, body any, checkRequest bool) (int, any) {
	c.t.Helper()
	op, ok := c.operations[operationID]
	require.True(c.t, ok, "the document has no operation %s", operationID)

	path := op.path
	for _, raw := range asList(op.definition["parameters"]) {
		parameter := raw.(map[string]any)
		if parameter["in"] != "path" {
			continue
		}
		name := parameter["name"].(string)
		value, given := pathParams[name]
		require.True(c.t, given, "%s needs path parameter %s", operationID, name)
		path = strings.ReplaceAll(path, "{"+name+"}", value)
	}
	require.NotContains(c.t, path, "{", "%s has an undeclared path parameter", operationID)

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(c.t, err)
		var generic any
		require.NoError(c.t, json.Unmarshal(encoded, &generic))
		schema := dig(op.definition, "requestBody", "content", "application/json", "schema")
		require.NotNil(c.t, schema, "%s documents no request body", operationID)
		problems := c.validate(schema, generic, "request", true)
		if checkRequest {
			require.Empty(c.t, problems, "%s: the request does not match the document", operationID)
		} else {
			require.NotEmpty(c.t, problems, "%s: the document should forbid this request", operationID)
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(op.method, c.baseURL+path+query, payload)
	require.NoError(c.t, err)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	documented, ok := op.definition["responses"].(map[string]any)[fmt.Sprint(resp.StatusCode)].(map[string]any)
	require.True(c.t, ok, "%s answered %d, which the document does not list: %s", operationID, resp.StatusCode, raw)
	if ref, isRef := documented["$ref"].(string); isRef {
		documented = c.resolve(ref)
	}
	var decoded any
	require.NoError(c.t, json.Unmarshal(raw, &decoded), "%s: %s", operationID, raw)
	schema := dig(documented, "content", "application/json", "schema")
	require.NotNil(c.t, schema, "%s documents no body for %d", operationID, resp.StatusCode)
	require.Empty(c.t, c.validate(schema, decoded, "response", true), "%s %d response does not match the document: %s", operationID, resp.StatusCode, raw)
	return resp.StatusCode, decoded
}

func (c *specClient) resolve(ref string) map[string]any {
	node := any(c.doc)
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		node = node.(map[string]any)[part]
	}
	require.NotNil(c.t, node, "unresolved %s", ref)
	return node.(map[string]any)
}

// validate checks value against the subset of JSON Schema the document uses.
// strict also rejects properties the schema does not declare.
func (c *specClient) validate(schema any, value any, at string, strict bool) []string {
	s, _ := schema.(map[string]any)
	if ref, ok := s["$ref"].(string); ok {
		return c.validate(c.resolve(ref), value, at, strict)
	}
	if options, ok := s["anyOf"].([]any); ok {
		for _, option := range options {
			if len(c.validate(option, value, at, strict)) == 0 {
				return nil
			}
		}
		return []string{at + ": matches none of anyOf"}
	}
	types := asStrings(s["type"])
	if len(types) == 0 {
		return nil // any JSON value
	}
	var kind string
	switch typed := value.(type) {
	case nil:
		kind = "null"
	case bool:
		kind = "boolean"
	case string:
		kind = "string"
	case float64:
		kind = "number"
		if typed == float64(int64(typed)) && contains(types, "integer") {
			kind = "integer"
		}
	case []any:
		kind = "array"
	case map[string]any:
		kind = "object"
	}
	if !contains(types, kind) {
		return []string{fmt.Sprintf("%s: is %s, want %v", at, kind, types)}
	}
	var problems []string
	switch typed := value.(type) {
	case string:
		if enum := asStrings(s["enum"]); len(enum) > 0 && !contains(enum, typed) {
			problems = append(problems, fmt.Sprintf("%s: %q is not one of %v", at, typed, enum))
		}
		switch s["format"] {
		case "uuid":
			if _, err := uuid.Parse(typed); err != nil {
				problems = append(problems, at+": not a UUID")
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339Nano, typed); err != nil {
				problems = append(problems, at+": not an RFC 3339 time")
			}
		}
		if limit, ok := s["minLength"].(float64); ok && float64(len(typed)) < limit {
			problems = append(problems, at+": too short")
		}
	case []any:
		for i, item := range typed {
			problems = append(problems, c.validate(s["items"], item, fmt.Sprintf("%s[%d]", at, i), strict)...)
		}
	case map[string]any:
		properties, _ := s["properties"].(map[string]any)
		for _, name := range asStrings(s["required"]) {
			if _, present := typed[name]; !present {
				problems = append(problems, at+"."+name+": required but missing")
			}
		}
		for name, item := range typed {
			property, declared := properties[name]
			if !declared {
				if strict && s["additionalProperties"] == nil {
					problems = append(problems, at+"."+name+": not declared in the document")
				}
				continue
			}
			problems = append(problems, c.validate(property, item, at+"."+name, strict)...)
		}
	}
	return problems
}

func dig(node any, keys ...string) any {
	for _, key := range keys {
		object, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node = object[key]
	}
	return node
}

func asList(value any) []any {
	list, _ := value.([]any)
	return list
}

func asStrings(value any) []string {
	if single, ok := value.(string); ok {
		return []string{single}
	}
	var out []string
	for _, item := range asList(value) {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// A client that knows only the served document creates an issue and comments
// on it with a personal access token, over real HTTP, auth, workspace
// middleware and PostgreSQL. Every response is checked against the document.
func TestOpenAPIClientCreatesIssueAndComment(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	user, workspace := uuid.New(), uuid.New()
	slug := "api-" + workspace.String()[:8]
	_, err = db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'API client','hash')`, user, user.String()+"@example.test")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id=$1`, user) })
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'API reference',$2,$3)`, workspace, slug, user)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM workspaces WHERE id=$1`, workspace) })
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, workspace, user)
	require.NoError(t, err)

	users := repository.NewUserRepository(db)
	workspaces := repository.NewWorkspaceRepository(db)
	teams := repository.NewTeamRepository(db)
	statuses := repository.NewTeamStatusRepository(db)
	projects := repository.NewProjectRepository(db)
	issues := repository.NewIssueRepository(db)
	tokens := repository.NewPersonalAccessTokenRepository(db)
	hub := realtime.NewHub(workspaces.RealtimeAccess)
	notifications := service.NewNotificationService(repository.NewNotificationRepository(db))
	issueSvc := service.NewIssueService(issues, teams, statuses, repository.NewIssueHistoryRepository(db), hub, notifications, projects)
	commentSvc := service.NewCommentService(repository.NewCommentRepository(db), issues, hub, notifications)
	relationSvc := service.NewIssueRelationService(repository.NewIssueRelationRepository(db), issues)

	secret := "openapi-client-test-secret"
	middleware := testMiddleware(mw.Auth(secret, tokens))
	middleware.workspaceMembership = mw.WorkspaceMembership(workspaces)
	e := echo.New()
	registerRoutes(e, &appHandlers{
		workspace:  handler.NewWorkspaceHandler(service.NewWorkspaceService(workspaces, users, hub)),
		team:       handler.NewTeamHandler(service.NewTeamService(teams, statuses, hub)),
		teamStatus: handler.NewTeamStatusHandler(service.NewTeamStatusService(statuses, repository.NewProjectStatusVisibilityRepository(db))),
		cycle:      handler.NewCycleHandler(service.NewCycleService(repository.NewCycleRepository(db), teams, hub, notifications)),
		issue:      handler.NewIssueHandler(issueSvc, commentSvc, users, statuses, projects, repository.NewCycleRepository(db), relationSvc),
		openapi:    handler.NewOpenAPIHandler("v0.0.1-test"),
	}, middleware)
	server := httptest.NewServer(e)
	defer server.Close()

	pat := func(scopes ...string) string {
		t.Helper()
		raw, hash, prefix, err := domain.GenerateToken()
		require.NoError(t, err)
		require.NoError(t, tokens.Create(context.Background(), &domain.PersonalAccessToken{ID: uuid.New(), UserID: user, Name: "API reference", TokenHash: hash, TokenPrefix: prefix, Scopes: pq.StringArray(scopes)}))
		return raw
	}
	client := newSpecClient(t, server.URL, pat("workspaces:read", "team:manage", "teams:read", "cycles:read", "issue:create", "issues:read", "comments:read"), server.Client())
	field := func(value any, name string) any { return value.(map[string]any)[name] }

	status, listed := client.call("listWorkspaces", nil, "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, listed, 1)
	require.Equal(t, slug, field(listed.([]any)[0], "slug"))
	in := map[string]string{"slug": slug}

	status, team := client.call("createTeam", in, "", map[string]any{"name": "Platform", "key": "API"})
	require.Equal(t, http.StatusCreated, status)
	teamID := field(team, "id").(string)

	status, teamStatuses := client.call("listStatuses", map[string]string{"slug": slug, "teamId": teamID}, "", nil)
	require.Equal(t, http.StatusOK, status)
	require.NotEmpty(t, teamStatuses, "a new team has default statuses")

	// Empty lists are arrays, never null, so generated clients can iterate them.
	status, velocity := client.call("getCycleVelocity", map[string]string{"slug": slug, "teamId": teamID}, "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []any{}, velocity)

	status, issue := client.call("createIssue", in, "", map[string]any{"title": "Created through the OpenAPI document", "team_id": teamID, "priority": 2, "description": "From a **generated** client."})
	require.Equal(t, http.StatusCreated, status)
	identifier := field(issue, "identifier").(string)
	require.Equal(t, "API-1", identifier)
	require.Equal(t, "Created through the OpenAPI document", field(issue, "title"))
	issuePath := map[string]string{"slug": slug, "identifier": identifier}

	status, comment := client.call("createIssueComment", issuePath, "", map[string]any{"body": "First comment from the API"})
	require.Equal(t, http.StatusCreated, status)
	require.Equal(t, field(issue, "id"), field(comment, "issue_id"))
	require.Equal(t, user.String(), field(comment, "user_id"))

	// The writes persisted and read back through the documented operations.
	status, fetched := client.call("getIssue", issuePath, "", nil)
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, 2, field(fetched, "priority"))
	status, comments := client.call("listIssueComments", issuePath, "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, comments, 1)
	require.Contains(t, field(comments.([]any)[0], "body"), "First comment from the API")
	status, page := client.call("listIssues", in, "?team="+teamID+"&per_page=10", nil)
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, 1, field(page, "total_count"))
	require.Equal(t, false, field(page, "has_more"))
	require.Len(t, field(page, "data"), 1)

	// Errors use the documented shape and statuses.
	status, invalid := client.send("createIssue", in, "", map[string]any{"team_id": teamID}, false)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "VALIDATION_ERROR", field(field(invalid, "error"), "code"))
	status, missing := client.call("getIssue", map[string]string{"slug": slug, "identifier": "API-999"}, "", nil)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, "NOT_FOUND", field(field(missing, "error"), "code"))

	// A token without the documented scope is refused, and nothing is written.
	readOnly := newSpecClient(t, server.URL, pat("workspaces:read", "issues:read", "comments:read"), server.Client())
	status, refused := readOnly.call("createIssueComment", issuePath, "", map[string]any{"body": "must not be stored"})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "FORBIDDEN", field(field(refused, "error"), "code"))
	var stored int
	require.NoError(t, db.Get(&stored, `SELECT COUNT(*) FROM comments WHERE issue_id=$1`, field(issue, "id")))
	require.Equal(t, 1, stored)
}
