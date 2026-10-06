package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v4"
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
	for _, r := range e.Routes() {
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
	for _, route := range e.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	baseline, err := os.ReadFile("testdata/integrated_routes.txt")
	require.NoError(t, err)
	for _, key := range strings.Split(strings.TrimSpace(string(baseline)), "\n") {
		require.True(t, registered[key], "integrated route %s was removed", key)
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
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			e := echo.New()
			auth := func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
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
				return func(c echo.Context) error {
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
