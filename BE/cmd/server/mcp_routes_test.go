package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/mcp"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// Every MCP tool and resource is a call to an API route. A path that matches
// no registered route would only fail when an agent calls the tool, and a
// route without a permission annotation would not honour token scopes, so
// both fail here: each must be a route registered with scoped().
func TestMCPToolsAndResourcesCallScopedRoutes(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))

	// Routes are compared by shape: parameter names differ between the two
	// sides (:slug and {workspace}), their positions must not.
	routeParam := regexp.MustCompile(`:[A-Za-z0-9_]+`)
	toolParam := regexp.MustCompile(`\{[a-z_]+\}`)
	annotations := map[string]string{}
	for _, r := range e.Router().Routes() {
		annotations[r.Method+" "+routeParam.ReplaceAllString(r.Path, "*")] = r.Name
	}

	routes := mcp.APIRoutes()
	require.NotEmpty(t, routes)
	for _, route := range routes {
		require.NotContains(t, route.Path, "*", "%s", route.Source)
		annotation, registered := annotations[route.Method+" "+toolParam.ReplaceAllString(route.Path, "*")]
		require.True(t, registered, "%s calls %s %s, which is not a registered route", route.Source, route.Method, route.Path)
		require.True(t, strings.HasPrefix(annotation, "perm:"), "%s calls %s %s, annotated %q: MCP may only reach token-scoped routes", route.Source, route.Method, route.Path, annotation)
	}
}
