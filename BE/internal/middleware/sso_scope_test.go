package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runSSOScope(t *testing.T, ssoWorkspace uuid.UUID, markPAT bool) (int, string) {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	c := e.NewContext(req, rec)
	if ssoWorkspace != uuid.Nil {
		c.Set(SessionSSOWorkspaceKey, ssoWorkspace)
	}
	if markPAT {
		c.Set(TokenScopesKey, []string{})
	}
	handler := RejectSSOScopedSession()(func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	require.NoError(t, handler(c))
	return rec.Code, rec.Body.String()
}

func TestRejectSSOScopedSession(t *testing.T) {
	code, body := runSSOScope(t, uuid.New(), false)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, `"code":"SSO_SESSION_SCOPED"`)

	code, _ = runSSOScope(t, uuid.Nil, false)
	assert.Equal(t, http.StatusNoContent, code)

	// A PAT authenticates without a session scope, so the guard passes it.
	code, _ = runSSOScope(t, uuid.Nil, true)
	assert.Equal(t, http.StatusNoContent, code)
}
