package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/openapi"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// Every route a personal access token can reach is either a documented
// operation of the public reference or explicitly classified as internal.
// Adding, removing, moving or re-scoping a token-accessible route fails here
// until internal/openapi is updated, which in turn changes the published
// document (see the openapi package's tests).
func TestTokenRoutesMatchThePublicReference(t *testing.T) {
	e := echo.New()
	registerRoutes(e, &appHandlers{}, testMiddleware(noopMiddleware()))
	annotations := map[string]string{}
	for _, route := range e.Router().Routes() {
		if route.Method != echo.RouteNotFound {
			annotations[route.Method+" "+route.Path] = route.Name
		}
	}

	internal := openapi.InternalRoutes()
	classified := func(method, path string) int {
		return len(slices.DeleteFunc(slices.Clone(internal), func(rule openapi.InternalRoute) bool { return !rule.Matches(method, path) }))
	}

	documented := map[string]bool{}
	for _, op := range openapi.Operations() {
		key := op.Method + " " + op.Path
		require.False(t, documented[key], "%s is documented twice", key)
		documented[key] = true
		annotation, registered := annotations[key]
		require.True(t, registered, "documented operation %s (%s) is not a registered route", op.ID, key)
		require.Equal(t, "perm:"+op.Scope, annotation, "documented operation %s (%s) must be token-accessible with the documented scope", op.ID, key)
		require.Zero(t, classified(op.Method, op.Path), "%s is both documented and classified as internal", key)
	}

	for key, annotation := range annotations {
		if !strings.HasPrefix(annotation, "perm:") || documented[key] {
			continue
		}
		method, path, _ := strings.Cut(key, " ")
		require.Equal(t, 1, classified(method, path),
			"token-accessible route %s must be documented in openapi.Operations or classified by exactly one openapi.InternalRoutes rule", key)
	}

	for _, rule := range internal {
		matches := 0
		for key, annotation := range annotations {
			method, path, _ := strings.Cut(key, " ")
			if strings.HasPrefix(annotation, "perm:") && rule.Matches(method, path) {
				matches++
			}
		}
		require.Positive(t, matches, "internal rule %s %s matches no token-accessible route; remove it", rule.Method, rule.Path)
		require.NotEmpty(t, rule.Reason)
	}
}

// The scopes the reference names for an operation are the ones the route's
// middleware enforces: all of them let a token through, and dropping any one
// is rejected before the handler runs.
func TestDocumentedScopesAreEnforced(t *testing.T) {
	param := regexp.MustCompile(`:[A-Za-z]+`)
	reaches := func(method, path string, scopes []string) bool {
		e := echo.New()
		auth := func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error {
				c.Set(mw.TokenScopesKey, scopes)
				c.Set("workspace_role", domain.RoleOwner)
				return next(c)
			}
		}
		registerRoutes(e, &appHandlers{}, testMiddleware(auth))
		rec := httptest.NewRecorder()
		panicked := false
		func() {
			// Handlers are nil: most requests the middleware lets through
			// panic, the rest fail on their empty body. Neither is a 403.
			defer func() { panicked = recover() != nil }()
			e.ServeHTTP(rec, httptest.NewRequest(method, param.ReplaceAllString(path, "x"), nil))
		}()
		return panicked || rec.Code != http.StatusForbidden
	}
	for _, op := range openapi.Operations() {
		scopes := append([]string{op.Scope}, op.AlsoRequires...)
		require.True(t, reaches(op.Method, op.Path, scopes), "%s: a token with %v must be allowed", op.ID, scopes)
		for i := range scopes {
			without := slices.Delete(slices.Clone(scopes), i, i+1)
			require.False(t, reaches(op.Method, op.Path, without), "%s: a token without %s must be rejected", op.ID, scopes[i])
		}
	}
}

// The reference is served without authentication and carries the release.
func TestOpenAPIDocumentIsServedPublicly(t *testing.T) {
	e := echo.New()
	rejectAll := func(echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { return c.NoContent(http.StatusUnauthorized) }
	}
	registerRoutes(e, &appHandlers{openapi: handler.NewOpenAPIHandler("v9.8.7")}, testMiddleware(rejectAll))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, "3.1.0", doc.OpenAPI)
	require.Equal(t, "9.8.7", doc.Info.Version)
	require.Contains(t, doc.Paths["/api/workspaces/{slug}/issues"], "post")
	require.Contains(t, doc.Paths["/api/workspaces/{slug}/issues/{identifier}/comments"], "post")
	for path := range doc.Paths {
		require.NotContains(t, path, "dev-machine", "internal routes are not published")
	}
}
