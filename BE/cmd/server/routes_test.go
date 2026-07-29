package main

import (
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
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
