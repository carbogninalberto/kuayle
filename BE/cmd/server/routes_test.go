package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func noopMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
}

// TestRoutePermissionAnnotations enforces deny-by-default for personal
// access tokens: the registration helpers in routes.go record each route's
// annotation as its Name at the same time they attach the middleware, so
// the two cannot drift apart. This test audits that every route carries a
// legal annotation and that every annotated permission code is a valid
// token scope. A route registered without the helpers (empty Name) fails.
func TestRoutePermissionAnnotations(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, &appMiddleware{
		auth:                   noopMiddleware(),
		authRateLimit:          noopMiddleware(),
		publicRateLimit:        noopMiddleware(),
		publicAssetRateLimit:   noopMiddleware(),
		workspaceMembership:    noopMiddleware(),
		devMachineDemoGuard:    noopMiddleware(),
		machineEventsRateLimit: noopMiddleware(),
		machineLogsRateLimit:   noopMiddleware(),
	})

	count := 0
	for _, r := range e.Router().Routes() {
		if r.Method == echo.RouteNotFound {
			continue // synthetic group entry, not a real route
		}
		count++
		require.NotEmpty(t, r.Name, "route %s %s has no permission annotation (use the scoped/sessionOnly/ownerOnly helpers or set an exemption Name)", r.Method, r.Path)
		switch {
		case r.Name == "public" || r.Name == "token" || r.Name == "session" || r.Name == "owner":
			// Explicit exemption categories.
		case strings.HasPrefix(r.Name, "perm:"):
			code := strings.TrimPrefix(r.Name, "perm:")
			require.True(t, domain.IsValidScope(code), "route %s %s annotated with unknown permission %q", r.Method, r.Path, code)
		default:
			t.Fatalf("route %s %s has invalid annotation %q", r.Method, r.Path, r.Name)
		}
	}
	require.Positive(t, count, "no routes registered")
}

// The independent manifest preserves all routes present before PAT extraction,
// including main's transfer endpoints and invite onboarding from PR #54.
func TestIntegratedRoutesPreserved(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))
	registered := map[string]bool{}
	for _, route := range e.Router().Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	baseline, err := os.ReadFile("testdata/integrated_routes.txt")
	require.NoError(t, err)
	for _, key := range strings.Split(strings.TrimSpace(string(baseline)), "\n") {
		require.True(t, registered[key], "integrated route %s was removed", key)
	}
}

// Agents run only as tasks; the manual "Run agent" routes stay retired.
func TestManualAgentRunRoutesAreRetired(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))
	for _, route := range e.Router().Routes() {
		require.NotContains(t, route.Path, "/agent-runs", "%s %s", route.Method, route.Path)
	}
}

func testMiddleware(auth echo.MiddlewareFunc) *appMiddleware {
	return &appMiddleware{
		auth: auth, authRateLimit: noopMiddleware(), publicRateLimit: noopMiddleware(),
		publicAssetRateLimit: noopMiddleware(), workspaceMembership: noopMiddleware(),
		devMachineDemoGuard: noopMiddleware(), machineEventsRateLimit: noopMiddleware(),
		machineLogsRateLimit: noopMiddleware(),
	}
}

func TestRegisteredRoutesRejectPATOutsideBoundaries(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		scopes       []string
		restricted   bool
	}{
		{"GET", "/api/tokens", []string{"account:read"}, false},
		{"POST", "/api/tokens", []string{"account:read"}, false},
		{"DELETE", "/api/tokens/example", []string{"account:read"}, false},
		{"POST", "/api/invite/example/accept", []string{"member:invite"}, false},
		{"POST", "/api/workspaces/import", []string{"workspace:transfer"}, false},
		{"POST", "/api/workspaces/import/preview", []string{"workspace:transfer"}, false},
		{"GET", "/api/workspaces/acme/export", []string{"workspaces:read"}, false},
		{"POST", "/api/workspaces/acme/invite-links", []string{"members:read"}, false},
		{"GET", "/api/workspaces/acme/invite-links", []string{"members:read"}, false},
		{"DELETE", "/api/workspaces/acme/invite-links/example", []string{"members:read"}, false},
		{"GET", "/api/notifications", []string{"notifications:read"}, true},
		{"GET", "/api/preferences", []string{"account:read"}, true},
		{"GET", "/api/workspaces/acme/shared-links", []string{"account:read"}, false},
		{"POST", "/api/workspaces/acme/dev-machines/example/services/terminal/launch", []string{"dev_machine:read"}, false},
		{"POST", "/api/workspaces/acme/dev-machines/example/terminal-sessions", []string{"dev_machine:read"}, false},
		{"POST", "/api/workspaces/acme/dev-machines/example/terminal-sessions/example/close", []string{"dev_machine:read"}, false},
		{"POST", "/api/workspaces/acme/dev-machines/example/activity", []string{"dev_machine:read"}, false},
		{"GET", "/api/workspaces/acme/agent-sign-ins", []string{"dev_machine:read"}, false},
		{"PUT", "/api/workspaces/acme/agent-sign-ins/claude-code/token", []string{"dev_machine:admin"}, false},
		{"DELETE", "/api/workspaces/acme/agent-sign-ins/claude-code", []string{"dev_machine:admin"}, false},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			e := echo.New()
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.Set(mw.TokenScopesKey, tc.scopes)
					c.Set("workspace_role", domain.RoleOwner)
					if tc.restricted {
						c.Set(mw.TokenWorkspacesKey, []string{"acme"})
					}
					return next(c)
				}
			}
			registerRoutes(e, &appHandlers{}, testMiddleware(auth))
			rec := httptest.NewRecorder()
			// Nil handlers intentionally panic if the route's middleware lets a
			// prohibited request through instead of rejecting it before DB access.
			e.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			require.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

// These routes were historically session-only, which does not constrain a guest.
// Exercise registered middleware before nil handlers can run.
func TestCycleWritesRejectGuestSessions(t *testing.T) {
	for _, target := range []struct{ method, path string }{
		{"POST", "/teams/team/cycles"},
		{"PATCH", "/teams/team/cycles/cycle"},
		{"POST", "/teams/team/cycles/cycle/complete"},
		{"DELETE", "/teams/team/cycles/cycle"},
	} {
		t.Run(target.method+target.path, func(t *testing.T) {
			e := echo.New()
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					c.Set("workspace_role", domain.RoleGuest)
					return next(c)
				}
			}
			registerRoutes(e, &appHandlers{}, testMiddleware(auth))
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, httptest.NewRequest(target.method, "/api/workspaces/example"+target.path, nil))
			require.Equal(t, http.StatusForbidden, recorder.Code)
		})
	}
}

// The GitHub App setup check exposes the app's owner and installation pages,
// so like the other GitHub settings it is limited to workspace managers.
func TestGitHubAppSetupRequiresWorkspaceManage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		role   string
		scopes []string
	}{
		{"admin session", domain.RoleAdmin, nil},
		{"member session", domain.RoleMember, nil},
		{"guest session", domain.RoleGuest, nil},
		{"owner token without workspace:manage", domain.RoleOwner, []string{"workspaces:read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error {
					if tc.scopes != nil {
						c.Set(mw.TokenScopesKey, tc.scopes)
					}
					c.Set("workspace_role", tc.role)
					return next(c)
				}
			}
			registerRoutes(e, &appHandlers{}, testMiddleware(auth))
			rec := httptest.NewRecorder()
			// The nil handler panics if the request gets past authorization.
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/acme/github/app-setup", nil))
			require.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

// Echo v5 rejects duplicate method+path registrations at startup, so the
// optional route sets must register cleanly alongside everything else.
func TestOptionalRouteSetsRegisterWithoutConflicts(t *testing.T) {
	e := echo.New()
	require.NotPanics(t, func() {
		registerRoutes(e, &appHandlers{
			telegram:        &handler.TelegramHandler{},
			agentAutomation: &handler.AgentAutomationHandler{},
			agentMessage:    &handler.AgentMessageHandler{},
		}, testMiddleware(noopMiddleware()))
	})
	registered := map[string]bool{}
	for _, route := range e.Router().Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["POST /api/telegram/webhook/:workspaceId"])
}

// Draft issues can be expanded with AI by anyone allowed to create issues;
// the static path must not be captured by the per-issue routes.
func TestDraftDescriptionExpansionRequiresIssueCreate(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))
	found := false
	for _, route := range e.Router().Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/workspaces/:slug/issues/expand-description" {
			found = true
			require.Equal(t, "perm:issue:create", route.Name)
		}
	}
	require.True(t, found, "draft expansion route is not registered")
}

// Wiki reads need wiki:read and every wiki write needs wiki:write, for both
// scopes, so guests and read-only tokens cannot change documentation.
func TestWikiRoutesRequireWikiPermissions(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))
	got := map[string]string{}
	for _, route := range e.Router().Routes() {
		if strings.Contains(route.Path, "/wiki") {
			got[route.Method+" "+route.Path] = route.Name
		}
	}
	want := map[string]string{}
	for _, base := range []string{"/api/workspaces/:slug/teams/:teamId/wiki", "/api/workspaces/:slug/projects/:id/wiki"} {
		want["GET "+base] = "perm:wiki:read"
		want["POST "+base] = "perm:wiki:write"
		want["GET "+base+"/:pageId"] = "perm:wiki:read"
		want["PATCH "+base+"/:pageId"] = "perm:wiki:write"
		want["DELETE "+base+"/:pageId"] = "perm:wiki:write"
		want["GET "+base+"/:pageId/versions"] = "perm:wiki:read"
		want["POST "+base+"/:pageId/rework"] = "perm:wiki:write"
		want["GET "+base+"/settings"] = "perm:wiki:read"
		want["PATCH "+base+"/settings"] = "perm:wiki:write"
		want["GET "+base+"/registry"] = "perm:wiki:read"
		want["POST "+base+"/registry"] = "perm:wiki:write"
		want["GET "+base+"/registry/overviews"] = "perm:wiki:read"
		want["POST "+base+"/registry/overviews"] = "perm:wiki:write"
		want["GET "+base+"/tree"] = "perm:wiki:read"
		want["POST "+base+"/tree"] = "perm:wiki:write"
		want["POST "+base+"/:pageId/move"] = "perm:wiki:write"
		want["GET "+base+"/:pageId/publish"] = "perm:wiki:read"
		want["PUT "+base+"/:pageId/publish"] = "perm:wiki:write"
		want["GET "+base+"/:pageId/backlinks"] = "perm:wiki:read"
	}
	// Page references: suggestions and chips need wiki:read; attaching a page
	// to an issue is an issue edit.
	want["GET /api/workspaces/:slug/wiki/references"] = "perm:wiki:read"
	want["POST /api/workspaces/:slug/wiki/references/resolve"] = "perm:wiki:read"
	want["GET /api/workspaces/:slug/issues/:identifier/wiki-references"] = "perm:issues:read"
	want["POST /api/workspaces/:slug/issues/:identifier/wiki-references"] = "perm:issue:update"
	want["DELETE /api/workspaces/:slug/issues/:identifier/wiki-references/:pageId"] = "perm:issue:update"
	// A public wiki link is anonymous and rate limited like issue shares.
	want["GET /api/public/wiki/:token"] = "public"
	require.Equal(t, want, got)
}

// Agent task and workflow reads are open to tokens with their own opt-in
// scopes (the Kuayle CLI); the repositories still apply task and member
// visibility. Writes and reads that can expose code or secrets (logs, diffs,
// artifacts, sessions, live messages) stay session-only.
func TestAgentTaskAndWorkflowReadRoutes(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))
	names := map[string]string{}
	for _, route := range e.Router().Routes() {
		names[route.Method+" "+route.Path] = route.Name
	}
	const ws = "/api/workspaces/:slug"
	for path, want := range map[string]string{
		"GET " + ws + "/agent-tasks":                               "perm:agent_tasks:read",
		"GET " + ws + "/agent-tasks/:taskId":                       "perm:agent_tasks:read",
		"GET " + ws + "/agent-tasks/:taskId/history":               "perm:agent_tasks:read",
		"GET " + ws + "/agent-tasks/:taskId/runs":                  "perm:agent_tasks:read",
		"GET " + ws + "/workflows":                                 "perm:workflows:read",
		"GET " + ws + "/workflows/:workflowId":                     "perm:workflows:read",
		"GET " + ws + "/agent-profiles":                            "perm:workflows:read",
		"GET " + ws + "/agent-tasks/:taskId/runs/:runId/logs":      "session",
		"GET " + ws + "/agent-tasks/:taskId/runs/:runId/diff":      "session",
		"GET " + ws + "/agent-tasks/:taskId/runs/:runId/artifacts": "session",
		"GET " + ws + "/agent-tasks/:taskId/sessions":              "session",
		"GET " + ws + "/agent-tasks/:taskId/live-messages":         "session",
		"POST " + ws + "/agent-tasks/:taskId/cancel":               "session",
		"POST " + ws + "/workflows":                                "session",
		"GET " + ws + "/workflows/:workflowId/audit":               "session",
	} {
		require.Equal(t, want, names[path], path)
	}
}

func TestAgentTaskAndWorkflowReadsRejectGuestsAndUnscopedTokens(t *testing.T) {
	for _, path := range []string{"/agent-tasks", "/agent-tasks/task", "/workflows", "/agent-profiles"} {
		for _, tc := range []struct {
			name   string
			role   string
			scopes []string
		}{
			{"guest session", domain.RoleGuest, nil},
			{"guest token with the scopes", domain.RoleGuest, []string{domain.PermAgentTasksRead, domain.PermWorkflowsRead}},
			{"owner token without the scopes", domain.RoleOwner, []string{domain.PermIssuesRead, domain.PermWikiRead}},
		} {
			t.Run(tc.name+" "+path, func(t *testing.T) {
				e := echo.New()
				auth := func(next echo.HandlerFunc) echo.HandlerFunc {
					return func(c *echo.Context) error {
						if tc.scopes != nil {
							c.Set(mw.TokenScopesKey, tc.scopes)
						}
						c.Set("workspace_role", tc.role)
						return next(c)
					}
				}
				registerRoutes(e, &appHandlers{}, testMiddleware(auth))
				rec := httptest.NewRecorder()
				// The nil handler panics if the request gets past authorization.
				e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/acme"+path, nil))
				require.Equal(t, http.StatusForbidden, rec.Code)
			})
		}
	}
}
