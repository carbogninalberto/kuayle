package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runMiddleware(t *testing.T, mw echo.MiddlewareFunc, setup func(c echo.Context)) int {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
	if setup != nil {
		setup(c)
	}
	require.NoError(t, mw(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})(c))
	return rec.Code
}

func patContext(scopes ...string) func(c echo.Context) {
	return func(c echo.Context) {
		c.Set(TokenScopesKey, scopes)
	}
}

func TestRequirePermissionPATWithinScopeAndRole(t *testing.T) {
	code := runMiddleware(t, RequirePermission("issue:create"), func(c echo.Context) {
		patContext("issue:create")(c)
		c.Set("workspace_role", domain.RoleMember)
	})
	assert.Equal(t, http.StatusNoContent, code)
}

func TestRequirePermissionPATBeyondScopeDenied(t *testing.T) {
	// Role allows it, but the token's scopes are the ceiling.
	code := runMiddleware(t, RequirePermission("issue:create"), func(c echo.Context) {
		patContext("issues:read")(c)
		c.Set("workspace_role", domain.RoleMember)
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestRequirePermissionPATBeyondRoleDenied(t *testing.T) {
	// Scope allows it, but the user's role is the floor.
	code := runMiddleware(t, RequirePermission("issue:create"), func(c echo.Context) {
		patContext("issue:create")(c)
		c.Set("workspace_role", domain.RoleGuest)
	})
	assert.Equal(t, http.StatusForbidden, code)
}

func TestRequirePermissionJWTUnaffected(t *testing.T) {
	withRole := runMiddleware(t, RequirePermission("issue:create"), func(c echo.Context) {
		c.Set("workspace_role", domain.RoleMember)
	})
	assert.Equal(t, http.StatusNoContent, withRole)

	withoutRole := runMiddleware(t, RequirePermission("issue:create"), func(c echo.Context) {
		c.Set("workspace_role", domain.RoleGuest)
	})
	assert.Equal(t, http.StatusForbidden, withoutRole)
}

func TestRequirePermissionUserLevelRoute(t *testing.T) {
	// User-level routes have no workspace role: JWT passes as before,
	// PATs are governed by the scope check alone.
	jwt := runMiddleware(t, RequirePermission("account:read"), nil)
	assert.Equal(t, http.StatusNoContent, jwt)

	patScoped := runMiddleware(t, RequirePermission("account:read"), patContext("account:read"))
	assert.Equal(t, http.StatusNoContent, patScoped)

	patUnscoped := runMiddleware(t, RequirePermission("account:read"), patContext("issues:read"))
	assert.Equal(t, http.StatusForbidden, patUnscoped)
}

func TestRequireSession(t *testing.T) {
	jwt := runMiddleware(t, RequireSession(), nil)
	assert.Equal(t, http.StatusNoContent, jwt)

	pat := runMiddleware(t, RequireSession(), patContext("issues:read"))
	assert.Equal(t, http.StatusForbidden, pat)
}

func TestRequireOwner(t *testing.T) {
	ownerID := uuid.New()
	setup := func(c echo.Context) {
		c.Set("workspace", &domain.Workspace{OwnerID: ownerID})
		c.Set(string(UserIDKey), ownerID)
	}

	owner := runMiddleware(t, RequireOwner(), setup)
	assert.Equal(t, http.StatusNoContent, owner)

	stranger := runMiddleware(t, RequireOwner(), func(c echo.Context) {
		c.Set("workspace", &domain.Workspace{OwnerID: ownerID})
		c.Set(string(UserIDKey), uuid.New())
	})
	assert.Equal(t, http.StatusForbidden, stranger)

	// The owner authenticating with a PAT is still rejected.
	patOwner := runMiddleware(t, RequireOwner(), func(c echo.Context) {
		setup(c)
		patContext("workspace:manage")(c)
	})
	assert.Equal(t, http.StatusForbidden, patOwner)
}

func TestRequireUnrestrictedTokenWorkspaces(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pat   bool
		slugs []string
		want  int
	}{
		{"session", false, nil, http.StatusNoContent},
		{"unrestricted PAT", true, nil, http.StatusNoContent},
		{"restricted PAT", true, []string{"acme"}, http.StatusForbidden},
		{"empty stored restriction", true, []string{}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := runMiddleware(t, RequireUnrestrictedTokenWorkspaces(), func(c echo.Context) {
				if tc.pat {
					c.Set(TokenWorkspacesKey, tc.slugs)
				}
			})
			assert.Equal(t, tc.want, code)
		})
	}
}

func TestRequireTokenWritePermissionPreservesSessionPolicy(t *testing.T) {
	assert.Equal(t, http.StatusNoContent, runMiddleware(t, RequireTokenPermission(domain.PermDevMachineManage), nil))
	assert.Equal(t, http.StatusForbidden, runMiddleware(t, RequireTokenPermission(domain.PermDevMachineManage), patContext(domain.PermDevMachineRead)))
	assert.Equal(t, http.StatusNoContent, runMiddleware(t, RequireTokenPermission(domain.PermDevMachineManage), patContext(domain.PermDevMachineRead, domain.PermDevMachineManage)))
}

func TestRequireTokenWritePermissionIntersectsWorkspaceRole(t *testing.T) {
	for _, tc := range []struct {
		role string
		want int
	}{
		{domain.RoleGuest, http.StatusForbidden}, {domain.RoleMember, http.StatusNoContent},
		{domain.RoleOwner, http.StatusNoContent},
	} {
		t.Run(tc.role, func(t *testing.T) {
			code := runMiddleware(t, RequireTokenPermission(domain.PermDevMachineManage), func(c echo.Context) {
				patContext(domain.PermDevMachineRead, domain.PermDevMachineManage)(c)
				c.Set("workspace_role", tc.role)
			})
			assert.Equal(t, tc.want, code)
		})
	}
}
