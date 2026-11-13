package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// updateTokenMatrix rewrites the generated section of docs/api-tokens.md.
var updateTokenMatrix = flag.Bool("update", false, "rewrite the generated token matrix section in docs/api-tokens.md")

const (
	tokenMatrixDocPath = "../../../docs/api-tokens.md"
	tokenMatrixStart   = "<!-- token-matrix:start -->"
	tokenMatrixEnd     = "<!-- token-matrix:end -->"
)

type registeredRoute struct {
	Method string
	Path   string
	Name   string
}

func allRegisteredRoutes(t *testing.T) []registeredRoute {
	t.Helper()
	e := echo.New()
	registerRoutes(e, matrixHandlers(), testMiddleware(noopMiddleware()))
	routes := make([]registeredRoute, 0, len(e.Router().Routes()))
	for _, route := range e.Router().Routes() {
		if route.Method == echo.RouteNotFound {
			continue // synthetic group entry, not a real route
		}
		routes = append(routes, registeredRoute{Method: route.Method, Path: route.Path, Name: route.Name})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes
}

// matrixHandlers registers the optional route sets too, so the matrix covers
// the complete API surface rather than only the always-on handlers.
func matrixHandlers() *appHandlers {
	return &appHandlers{
		telegram:        &handler.TelegramHandler{},
		agentAutomation: &handler.AgentAutomationHandler{},
		agentMessage:    &handler.AgentMessageHandler{},
		memory:          &handler.WorkspaceMemoryHandler{},
		agentAssignee:   &handler.AgentAssigneeHandler{},
	}
}

var pathParam = regexp.MustCompile(`:[A-Za-z]+`)

// requestReaches reports whether a request with the given token scopes gets
// past the route's middleware. Handlers are nil, so a request that gets
// through panics or returns a non-403 status; both count as reached.
func requestReaches(t *testing.T, method, path string, scopes []string, role string) bool {
	t.Helper()
	e := echo.New()
	auth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if scopes != nil {
				c.Set(mw.TokenScopesKey, scopes)
			}
			c.Set("workspace_role", role)
			return next(c)
		}
	}
	registerRoutes(e, matrixHandlers(), testMiddleware(auth))
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(method, pathParam.ReplaceAllString(path, "example"), nil)
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		e.ServeHTTP(rec, request)
	}()
	return panicked || rec.Code != http.StatusForbidden
}

// Every route annotated with a permission must be reachable by an owner token
// holding all valid scopes, and must reject a token that lacks exactly that
// route's annotated scope. This is the executable form of the scope matrix.
func TestTokenRoutesAllowAnnotatedScope(t *testing.T) {
	allScopes := domain.TokenScopes()
	require.NotEmpty(t, allScopes)
	for _, route := range allRegisteredRoutes(t) {
		if !strings.HasPrefix(route.Name, "perm:") {
			continue
		}
		scope := strings.TrimPrefix(route.Name, "perm:")
		t.Run(strings.ReplaceAll(route.Method+" "+route.Path, "/", "_"), func(t *testing.T) {
			require.True(t, requestReaches(t, route.Method, route.Path, allScopes, domain.RoleOwner),
				"%s %s: an owner token with all scopes must be allowed", route.Method, route.Path)
			without := make([]string, 0, len(allScopes)-1)
			for _, candidate := range allScopes {
				if candidate != scope {
					without = append(without, candidate)
				}
			}
			require.False(t, requestReaches(t, route.Method, route.Path, without, domain.RoleOwner),
				"%s %s: a token without %s must be rejected", route.Method, route.Path, scope)
		})
	}
}

// Session-only and owner-only routes must stay out of reach of a token even
// when it holds every scope.
func TestSessionAndOwnerRoutesRejectTokens(t *testing.T) {
	allScopes := domain.TokenScopes()
	for _, route := range allRegisteredRoutes(t) {
		if route.Name != "session" && route.Name != "owner" {
			continue
		}
		t.Run(strings.ReplaceAll(route.Method+" "+route.Path, "/", "_"), func(t *testing.T) {
			require.False(t, requestReaches(t, route.Method, route.Path, allScopes, domain.RoleOwner),
				"%s %s (%s) must reject personal access tokens", route.Method, route.Path, route.Name)
		})
	}
}

func reasonForRoute(method, path string) (string, bool) {
	for _, rule := range sessionOnlyReasons {
		if rule.Method != "" && rule.Method != method {
			continue
		}
		if rule.Exact {
			if path == rule.PathPrefix {
				return rule.Reason, true
			}
			continue
		}
		if strings.HasPrefix(path, rule.PathPrefix) {
			return rule.Reason, true
		}
	}
	return "", false
}

// Every session-only route needs a written reason, and every rule must cover
// a real route so the table cannot rot.
func TestSessionOnlyRoutesHaveReasons(t *testing.T) {
	matched := make(map[string]bool)
	missing := make([]string, 0)
	for _, route := range allRegisteredRoutes(t) {
		if route.Name != "session" {
			continue
		}
		if reason, ok := reasonForRoute(route.Method, route.Path); ok {
			require.NotEmpty(t, reason)
			matched[route.Method+" "+route.Path] = true
			continue
		}
		missing = append(missing, fmt.Sprintf("%s %s", route.Method, route.Path))
	}
	require.Empty(t, missing, "session-only routes without a reason rule:\n%s", strings.Join(missing, "\n"))

	for _, rule := range sessionOnlyReasons {
		require.NotEmpty(t, rule.PathPrefix)
		require.NotEmpty(t, rule.Reason)
		matches := 0
		for _, route := range allRegisteredRoutes(t) {
			if route.Name != "session" {
				continue
			}
			if rule.Method != "" && rule.Method != route.Method {
				continue
			}
			if rule.Exact {
				if route.Path == rule.PathPrefix {
					matches++
				}
				continue
			}
			if strings.HasPrefix(route.Path, rule.PathPrefix) {
				matches++
			}
		}
		require.Positive(t, matches, "session-only reason rule %s %q matches no session-only route", rule.Method, rule.PathPrefix)
	}
}

func matrixAccess(route registeredRoute) (access, scope string) {
	switch {
	case route.Name == "public":
		return "public", ""
	case route.Name == "token":
		return "token only", "the caller's own token"
	case route.Name == "owner":
		return "session only", "workspace owner"
	case route.Name == "session":
		reason, ok := reasonForRoute(route.Method, route.Path)
		if !ok {
			reason = "UNREASONED"
		}
		return "session only", reason
	case strings.HasPrefix(route.Name, "perm:"):
		return "token", strings.TrimPrefix(route.Name, "perm:")
	default:
		return "unknown", route.Name
	}
}

func renderTokenMatrix(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(tokenMatrixStart + "\n")
	b.WriteString("<!-- Generated by TestTokenMatrixDocumentation in BE/cmd/server. Do not edit by hand. -->\n")
	b.WriteString("<!-- Run `cd BE && go test ./cmd/server -run TestTokenMatrixDocumentation -update` to refresh. -->\n\n")
	b.WriteString("| Method | Path | Access | Scope / reason |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, route := range allRegisteredRoutes(t) {
		access, scope := matrixAccess(route)
		scope = strings.ReplaceAll(scope, "|", "\\|")
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", route.Method, route.Path, access, scope)
	}
	b.WriteString("\n" + tokenMatrixEnd)
	return b.String()
}

// The checked-in matrix must match the registered routes. Keep the matrix in
// docs/api-tokens.md current with -update when a route or scope changes.
func TestTokenMatrixDocumentation(t *testing.T) {
	content, err := os.ReadFile(tokenMatrixDocPath)
	require.NoError(t, err, "docs/api-tokens.md must exist")

	start := strings.Index(string(content), tokenMatrixStart)
	end := strings.Index(string(content), tokenMatrixEnd)
	require.GreaterOrEqual(t, start, 0, "docs/api-tokens.md is missing %s", tokenMatrixStart)
	require.Greater(t, end, start, "docs/api-tokens.md is missing %s", tokenMatrixEnd)

	generated := renderTokenMatrix(t)
	current := string(content[start : end+len(tokenMatrixEnd)])
	if *updateTokenMatrix {
		updated := string(content[:start]) + generated + string(content[end+len(tokenMatrixEnd):])
		require.NoError(t, os.WriteFile(tokenMatrixDocPath, []byte(updated), 0o644))
		return
	}
	require.Equal(t, generated, current,
		"docs/api-tokens.md token matrix is stale; run `cd BE && go test ./cmd/server -run TestTokenMatrixDocumentation -update`")
}

// The matrix must stay complete: every registered route appears exactly once.
func TestTokenMatrixCoversEveryRoute(t *testing.T) {
	routes := allRegisteredRoutes(t)
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		require.False(t, seen[key], "route %s is registered twice", key)
		seen[key] = true
	}
}
