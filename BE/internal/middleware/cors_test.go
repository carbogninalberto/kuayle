package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func corsPreflight(t *testing.T, frontendURL, origin string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.Use(CORS(frontendURL))
	e.GET("/api/config", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodOptions, "/api/config", nil)
	request.Header.Set(echo.HeaderOrigin, origin)
	request.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodGet)
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

func TestCORSAllowsOnlyTheFrontendOriginWithCredentials(t *testing.T) {
	allowed := corsPreflight(t, "https://app.example.com", "https://app.example.com")
	require.Equal(t, http.StatusNoContent, allowed.Code)
	require.Equal(t, "https://app.example.com", allowed.Header().Get(echo.HeaderAccessControlAllowOrigin))
	require.Equal(t, "true", allowed.Header().Get(echo.HeaderAccessControlAllowCredentials))
	require.Equal(t, "Content-Type,Authorization", allowed.Header().Get(echo.HeaderAccessControlAllowHeaders))

	denied := corsPreflight(t, "https://app.example.com", "https://evil.example.com")
	require.Empty(t, denied.Header().Get(echo.HeaderAccessControlAllowOrigin))
}

// A FRONTEND_URL that is not a bare origin (e.g. a trailing slash) must not
// stop the server; it matches no browser origin, as before.
func TestCORSWithNonOriginFrontendURLAllowsNoOrigin(t *testing.T) {
	var recorder *httptest.ResponseRecorder
	require.NotPanics(t, func() {
		recorder = corsPreflight(t, "https://app.example.com/", "https://app.example.com")
	})
	require.Empty(t, recorder.Header().Get(echo.HeaderAccessControlAllowOrigin))
}
